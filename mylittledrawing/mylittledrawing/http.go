// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// On App Engine, the framework sets up main; we should be a different package.
package mylittledrawing

import (
	"appengine"
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

var TESTING=true

var formatters = template.FuncMap{
	"time": timeFormatter,
}

func initTemplateSet() *template.Set {
	const templateFile = "template.html"
	bytes, err := ioutil.ReadFile(templateFile)
	if err != nil {
		panic("can't read templates: " + err.String())
	}
	set := template.NewSet().Funcs(formatters)
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

type Elem struct {
	Text string
	ImageKey string
	ConvKey string
	Time datastore.Time
	User string
}

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
	Template string
	Title string
	Data interface{}
}

func execute(w http.ResponseWriter, name, title string, data interface{}) {
	var b bytes.Buffer
	err := set.Execute("main", &b, &Executor{name, title, data})
	check(err)
	b.WriteTo(w)
}

// root is the HTTP handler for showing the landing page.
func root(w http.ResponseWriter, r *http.Request) {
	c := appengine.NewContext(r)
	getUser(c)
	if r.URL.RawPath != "/" {
		// TODO: 404
		panic(fmt.Errorf("no such page: %s", r.URL.String()))
	}
	// Get the list of conversations
	query := datastore.NewQuery("Conversation").
		Order("ModTime")
	var convs []*Conversation
	keys, err := query.GetAll(c, &convs)
	check(err)
	_ = keys
	execute(w, "root", "Home", convs)
}

// conversation is the HTTP handler to present and edit a conversation.
func conversation(w http.ResponseWriter, r *http.Request) {
	c := appengine.NewContext(r)
	getUser(c)
	keyString := r.FormValue("key")
	key := datastore.NewKey("Conversation", keyString, 0, nil)
	conv := getConv(c, key)
	execute(w, "conversation", conv.Title, conv)
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

// newElem is the HTTP handler to add an element to a conversation
func newElem(w http.ResponseWriter, r *http.Request) {
	addElem(w, r, r.FormValue("text"), "")
}

func addElem(w http.ResponseWriter, r *http.Request, text string, imageKey string) {
	c := appengine.NewContext(r)
	user := getUser(c)
	keyString := r.FormValue("key")
	if text == "" && imageKey == "" {
		check(fmt.Errorf("nothing in conversation: TODO"))
	}
	// Grab the conversation
	convKey := datastore.NewKey("Conversation", keyString, 0, nil)
	conv := new(Conversation)
	err := datastore.Get(c, convKey, conv)
	check(err)
	// Now store the element.
	elemKeyString := keyOf(text+imageKey) // TODO: THIS IS NOT RIGHT!! needs to be unique (include time?)
	// use convKey as the parent key to be available as the ancestor data for the query
	elemKey := datastore.NewKey("Elem", elemKeyString, 0, convKey)
	modTime := datastore.SecondsToTime(time.Seconds())
	elem := &Elem{
		Text: text,
		ImageKey: imageKey,
		ConvKey: keyString,
		Time: modTime,
		User: user,
	}
	_, err = datastore.Put(c, elemKey, elem)
	check(err)

	// Update the conversation
	conv.ModTime = modTime
	conv.ModUser = user
	_, err = datastore.Put(c, convKey, conv)
	check(err)

	// Reload the conversation from the data store.
	conv = getConv(c, convKey)
	// Render the list without the surrounding boilerplate.
	var b bytes.Buffer
	err = set.Execute("list", &b, conv)
	check(err)
	b.WriteTo(w)
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

	// Resize if too large, for more efficient moustachioing.
	// We aim for less than 1200 pixels in any dimension; if the
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
println("ENCODED")

	// Create an App Engine context for the client's request.
	c := appengine.NewContext(r)

	// Save the image under a unique key, a hash of the image.
	keyString := keyOf(buf.String())
	key := datastore.NewKey("Image", keyString, 0, nil)
	_, err = datastore.Put(c, key, &Image{buf.Bytes()})
	check(err)
println("WRITTEN")

	addElem(w, r, "", keyString)
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
	format := "Jan 06 3:04PM MST"
	switch {
	case now - then < 12*3600:
		format = "3:04PM MST"
	case now - then < 7*24*3600:
		format = "Mon 3:04PM MST"
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
