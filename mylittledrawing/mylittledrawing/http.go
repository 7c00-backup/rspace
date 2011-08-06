// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mylittledrawing

import (
	"appengine"
	"appengine/channel"
	"appengine/datastore"
	"appengine/user"
	"bytes"
	"crypto/sha1"
	"fmt"
	"http"
	"image"
	"image/jpeg"
	"io"
	"io/ioutil"
	"os"
	"resize"
	"runtime/debug"
	"strings"
	"exp/template"
	"time"
	_ "image/png" // import so we can read PNG files.
)

const TESTING = false

var formatters = template.FuncMap{
	"time": timeFormatter,
}

func initTemplateSet() *template.Set {
	const templateFile = "template.html"
	bytes, err := ioutil.ReadFile(templateFile)
	if err != nil {
		panic("can't read templates: " + err.String())
	}
	set := new(template.Set).Funcs(formatters)
	err = set.Parse(string(bytes))
	if err != nil {
		panic("can't parse templates: " + err.String())
	}
	return set
}

var (
	set = initTemplateSet()
)

// Because App Engine owns main and starts the HTTP service,
// we do our setup during initialization.
func init() {
	os.Setenv("TZ", "EST")
	http.HandleFunc("/", errorHandler(root))
	http.HandleFunc("/newconv", errorHandler(newConversation))
	http.HandleFunc("/newElem", errorHandler(newElem))
	http.HandleFunc("/conv", errorHandler(conversation))
	http.HandleFunc("/img", errorHandler(img))
	http.HandleFunc("/upload", errorHandler(upload))
	// for Channel management
	http.HandleFunc("/_ah/channel/connected/", errorHandler(viewerConnect))
	http.HandleFunc("/_ah/channel/disconnected/", errorHandler(viewerDisconnect))
}

// Conversation is the type used to hold the conversations in the datastore.
type Conversation struct {
	Title string
	CreateTime datastore.Time
	ModTime datastore.Time
	ModUser string
	Key string
	Elem []*Elem // always empty in the data store. TODO: don't use this type
}

type ConversationView struct {
	Conv *Conversation
	Token string // identifies the channel
}

type Elem struct {
	Text string
	ImageKey string
	ConvKey string
	Time datastore.Time
	User string
}

// Display is called from the template to generate the HTML representing an Elem. 
func (e *Elem) Display() string {
	var b bytes.Buffer
	template.HTMLEscape(&b, []byte(e.Text))
	if e.ImageKey != "" {
		// don't escape, just check
		ok := true
		for _, c := range e.ImageKey {
			switch {
			case '0' <= c && c <= '9':
			case 'a' <= c && c <= 'f':
			case 'A' <= c && c <= 'F':
			default:
				ok = false
			}
		}
		if ok {
			fmt.Fprintf(&b, `<img src="/img?key=%s"/>`, e.ImageKey)
		}
	}
	return b.String()
}

func getUser(c appengine.Context) string {
	u := user.Current(c)
	if u == nil {
		if TESTING {
			return "rob"
		}
		panic(fmt.Errorf("not signed in")) // TODO
	}
	return u.String()
}

type Executor struct {
	Title string
	Data interface{}
}

// Viewers holds the channel client ids for the viewers of a conversation.
// Stored in the datastore under the key of the conversation.
type Viewers struct {
	Client []string
}

func (v *Viewers) Notify(c appengine.Context) {
	for _, client := range v.Client {
		err := channel.Send(c, client, "update") // value unimportant; just a signal
		if err != nil {
			c.Infof("channel send: %s", err)
		}
	}
}

func updateViewers(c appengine.Context, convKeyStringID string, f func(*Viewers)) os.Error {
	convKey := datastore.NewKey("Conversation", convKeyStringID, 0, nil)
	viewersKey := datastore.NewKey("Viewers", "viewers-"+convKeyStringID, 0, convKey)
	var v Viewers
	err := datastore.Get(c, viewersKey, &v)
	if err != nil && err != datastore.ErrNoSuchEntity {
		return err
	}
	f(&v)
	if len(v.Client) == 0 {
		// delete viewer
		err = datastore.Delete(c, viewersKey)
	} else {
		_, err = datastore.Put(c, viewersKey, &v)
	}
	return err
}

func viewerConnect(w http.ResponseWriter, r *http.Request) {
	// nothing to do here?
}

func viewerDisconnect(w http.ResponseWriter, r *http.Request) {
	c := appengine.NewContext(r)
	clientID := r.FormValue("from")
	keyStringID := strings.Split(clientID, "/", -1)[0]
c.Infof("disconnnect from %q key %q", clientID, keyStringID)
	delViewer := func(c appengine.Context) os.Error {
		return updateViewers(c, keyStringID, func(v *Viewers){
			var nClient []string
			for _, id := range v.Client {
				if id != clientID {
					nClient = append(nClient, id)
				}
			}
			v.Client= nClient
		})
	}
	err := datastore.RunInTransaction(c, delViewer)
	if err != nil {
		c.Errorf("error in viewerDisconnect: %v", err)
		return
	}
}

func execute(w http.ResponseWriter, name, title string, data interface{}) {
	var b bytes.Buffer
	err := set.Execute(&b, name, &Executor{title, data})
	check(err)
	b.WriteTo(w)
}

// root is the HTTP handler for showing the landing page.
func root(w http.ResponseWriter, r *http.Request) {
	c := appengine.NewContext(r)
	getUser(c)
	if r.URL.RawPath != "/" {
		http.Error(w, "no such page: " + r.URL.Path, 404)
		return
	}
	// Get the list of conversations
	query := datastore.NewQuery("Conversation").
		Order("-ModTime")
	var convs []*Conversation
	keys, err := query.GetAll(c, &convs)
	check(err)
	_ = keys
	execute(w, "root", "Home", convs)
}

// Render the list without the surrounding boilerplate.
func renderList(w io.Writer, conv *Conversation) {
	var b bytes.Buffer
	check(set.Execute(&b, "list", conv))
	b.WriteTo(w)
}

// conversation is the HTTP handler to present and edit a conversation.
func conversation(w http.ResponseWriter, r *http.Request) {
	c := appengine.NewContext(r)
	user := getUser(c)
	keyString := r.FormValue("key")
	listOnly := r.FormValue("listOnly") == "true"
	key := datastore.NewKey("Conversation", keyString, 0, nil)
	conv := getConv(c, key)
	if listOnly {
		renderList(w, conv)
		return
	}
	// Channel nonsense
	clientID := key.StringID()+"/"+user // TODO: probably want a better name
	token, err := channel.Create(c, clientID)
	check(err)
	addViewer := func(c appengine.Context) os.Error {
		return updateViewers(c, key.StringID(), func(v *Viewers){
			v.Client= append(v.Client, clientID)
		})
	}
	err = datastore.RunInTransaction(c, addViewer)
	check(err)
	// end of channel nonsense
	cview := &ConversationView{conv, token}
	execute(w, "conversation", conv.Title, cview)
}

// getConv returns the conversation with the given key.
func getConv(c appengine.Context, key *datastore.Key) *Conversation {
	conv := new(Conversation)
	err := datastore.Get(c, key, conv)
	check(err)
	query := datastore.NewQuery("Elem").
		Filter("ConvKey=", key.StringID()).
		Order("Time").
		Ancestor(key) // provided when we create elemKey
	var elems []*Elem
	_, err = query.GetAll(c, &elems)
	check(err)
	conv.Elem = elems
	return conv
}

// newconversation is the HTTP handler to create a new conversation.
func newConversation(w http.ResponseWriter, r *http.Request) {
	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		panic(fmt.Errorf("conversation needs a title"))
	}
	// Is there already a conversation with that title?
	// TODO: shouldn't be a problem, checking this just for practice.
	// Store the conversation under a unique key. To keep it safe, we use the
	// hash of the title.
	c := appengine.NewContext(r)
	user := getUser(c)
	keyString := keyOf(title)
	key := datastore.NewKey("Conversation", keyString, 0, nil)
	conv := new(Conversation)
	err := datastore.Get(c, key, conv)
	if err == nil {
		panic(fmt.Errorf("there is already a conversation with that title"))
	}
	conv.Title = title
	conv.Key = keyString
	conv.CreateTime = datastore.SecondsToTime(time.Seconds())
	conv.ModTime = conv.CreateTime
	conv.ModUser = user
	// Save the conversation under a unique key. To keep it safe, we use the
	// hash of the title.
	_, err = datastore.Put(c, key, conv)
	check(err)
	// Redirect to show conversation.
	http.Redirect(w, r, "/conv?key="+keyString, 302)
}

// newElem is the HTTP handler to add an element to a conversation and redraw the page.
func newElem(w http.ResponseWriter, r *http.Request) {
	c := appengine.NewContext(r)
	convKey := addElem(w, r, r.FormValue("text"), "")
	// Reload the conversation from the data store.
	conv := getConv(c, convKey)
	renderList(w, conv)
}

func getConvKey(r *http.Request) *datastore.Key {
	return datastore.NewKey("Conversation", r.FormValue("key"), 0, nil)
}

func addElem(w http.ResponseWriter, r *http.Request, text string, imageKey string) (convKey *datastore.Key){
	if text == "" && imageKey == "" {
		check(fmt.Errorf("nothing in conversation: TODO"))
	}
	c := appengine.NewContext(r)
	user := getUser(c)
	// Grab the conversation
	convKey = getConvKey(r)
	conv := new(Conversation)
	err := datastore.Get(c, convKey, conv)
	check(err)
	// Now store the element.
	elemKeyString := keyOf(text+imageKey+fmt.Sprint(time.Nanoseconds()))
	// use convKey as the parent key to be available as the ancestor data for the query
	elemKey := datastore.NewKey("Elem", elemKeyString, 0, convKey)
	modTime := datastore.SecondsToTime(time.Seconds())
	elem := &Elem{
		Text: text,
		ImageKey: imageKey,
		ConvKey: convKey.StringID(),
		Time: modTime,
		User: user,
	}
	_, err = datastore.Put(c, elemKey, elem)
	check(err)

	// Update the conversation.
	conv.ModTime = modTime
	conv.ModUser = user
	_, err = datastore.Put(c, convKey, conv)
	check(err)

	// Send updates to all viewers.
	viewersKey := datastore.NewKey("Viewers", "viewers-"+convKey.StringID(), 0, convKey)
	v := new(Viewers)
	err = datastore.Get(c, viewersKey, v)
	check(err)
	v.Notify(c)
	return
}

// Image is the type used to hold the image in the datastore.
type Image struct {
	Data []byte
}

// upload is the HTTP handler for uploading images; it handles "/".
func upload(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		panic(fmt.Errorf("upload must be POST"))
	}

	f, _, err := r.FormFile("image")
	if err == http.ErrMissingFile {
		check(os.NewError("no file specified for upload"))
	}
	check(err)
	defer f.Close()

	// Grab the image data
	var buf bytes.Buffer
	io.Copy(&buf, f)
	i, _, err := image.Decode(&buf)
	check(err)

	// Resize if too large..
	// We aim for fewer than 1200 pixels in any dimension; if the
	// picture is larger than that, we squeeze it down to 600.
	const max = 1200
	if b := i.Bounds(); b.Dx() > max || b.Dy() > max {
		// If it's gigantic, it's more efficient to downsample first
		// and then resize; resizing will smooth out the roughness.
		if b.Dx() > 2*max || b.Dy() > 2*max {
			w, h := max, max
			if b.Dx() > b.Dy() {
				h = b.Dy() * h / b.Dx()
			} else {
				w = b.Dx() * w / b.Dy()
			}
			i = resize.Resample(i, i.Bounds(), w, h)
			b = i.Bounds()
		}
		w, h := max/2, max/2
		if b.Dx() > b.Dy() {
			h = b.Dy() * h / b.Dx()
		} else {
			w = b.Dx() * w / b.Dy()
		}
		i = resize.Resize(i, i.Bounds(), w, h)
	}

	// Encode as a new JPEG image.
	buf.Reset()
	err = jpeg.Encode(&buf, i, nil)
	check(err)

	// Create an App Engine context for the client's request.
	c := appengine.NewContext(r)

	// Save the image under a unique key, a hash of the image.
	keyString := keyOf(buf.String())
	key := datastore.NewKey("Image", keyString, 0, nil) // TODO: should nil be getConvKey(r)?
	_, err = datastore.Put(c, key, &Image{buf.Bytes()})
	check(err)

	convKey := addElem(w, r, "", keyString)

	// Redirect to show conversation.
	http.Redirect(w, r, "/conv?key="+convKey.StringID(), 302)
}

// keyOf returns (part of) the SHA-1 hash of the data, as a hex string.
func keyOf(data string) string {
	sha := sha1.New()
	sha.Write([]byte(data))
	return fmt.Sprintf("%x", string(sha.Sum())[0:16])
}

// img is the HTTP handler for displaying images and painting moustaches;
// it handles "/img".
func img(w http.ResponseWriter, r *http.Request) {
	c := appengine.NewContext(r)
	key := datastore.NewKey("Image", r.FormValue("key"), 0, nil)
	im := new(Image)
	err := datastore.Get(c, key, im)
	check(err)

	m, _, err := image.Decode(bytes.NewBuffer(im.Data))
	check(err)

	w.Header().Set("Content-type", "image/jpeg")
	jpeg.Encode(w, m, nil)
}

func timeFormatter(dt datastore.Time) string {
	now := time.Seconds()
	then := int64(dt)/1e6 // datastore times are in microseconds
	t := time.SecondsToLocalTime(then)
	format := "Jan 06 3:04PM"
	switch {
	case now - then < 12*3600:
		format = "3:04PM"
	case now - then < 7*24*3600:
		format = "Mon 3:04PM"
	}
	return fmt.Sprintf("@%s", t.Format(format))
}

// errorHandler wraps the argument handler with an error-catcher that
// returns a 500 HTTP error if the request fails (calls check with err non-nil).
func errorHandler(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err, ok := recover().(os.Error); ok {
				w.WriteHeader(http.StatusInternalServerError)
				execute(w, "error", "Error", err)
				trace := debug.Stack()
				txt := fmt.Sprintf("<pre>%s</pre>", trace)
				fmt.Fprint(w, txt)
				fmt.Fprint(os.Stderr, txt)
			}
		}()
		fn(w, r)
	}
}

// check aborts the current execution if err is non-nil.
func check(err os.Error) {
	if err != nil {
		panic(err)
	}
}
