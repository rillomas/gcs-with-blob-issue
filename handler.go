package gcs_with_blob_issue

import (
	"appengine"
	"appengine/blobstore"
	"appengine/image"
	"encoding/json"
	"fmt"
	"github.com/gorilla/mux"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	gappengine "google.golang.org/appengine"
	"google.golang.org/appengine/urlfetch"
	"google.golang.org/cloud"
	"google.golang.org/cloud/storage"
	"html/template"
	"io/ioutil"
	"mime"
	"net/http"
	"net/url"
	"path"
)

const (
	rootDirectory string = "web"
	indexFileName string = "index.html"

	tempImagePath       string = "image/temp/" // Path for temporary uploaded images
	createTempImagePath string = "create"      // Path for temporary uploaded images (while creating room)
	bucketName          string = "yourbucketname.appspot.com"

	uploadImageKey string = "image" // Form key of uploaded image
)

var tmpl *template.Template = template.Must(
	template.New("").ParseFiles(fmt.Sprintf("%s/%s", rootDirectory, indexFileName)))

// A proxy that creates an appengine context and handles it to the handler
type ContextHandler struct {
	Handler func(appengine.Context, http.ResponseWriter, *http.Request)
}

// Information about a served image
type ImageInfo struct {
	Key appengine.BlobKey
	Url string
}

func (f ContextHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c := appengine.NewContext(r)
	f.Handler(c, w, r)
}

// setup handlers
func init() {
	r := mux.NewRouter()
	r.Handle("/", ContextHandler{handleRootPageRequest})
	r.Handle("/api/1/uploadImage", ContextHandler{handleUploadImage}).Methods("POST")
	http.Handle("/", r)
}

func handleRootPageRequest(c appengine.Context, w http.ResponseWriter, r *http.Request) {
	err := tmpl.ExecuteTemplate(w, indexFileName, nil)
	if err != nil {
		c.Errorf("%v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func getImageUrl(c appengine.Context, key appengine.BlobKey) (*url.URL, error) {
	// get image url only if it was specified
	option := image.ServingURLOptions{
		Secure: true, // serve on https
	}
	return image.ServingURL(c, key, &option)
}

func handleUploadImage(c appengine.Context, w http.ResponseWriter, r *http.Request) {
	file, fileHeader, err := r.FormFile(uploadImageKey)
	if err != nil {
		c.Infof("Failed to get uploaded image: %v", err)
		http.Error(w, "Failed to get uploaded image", http.StatusBadRequest)
		return
	}
	defer file.Close()
	data, err := ioutil.ReadAll(file)
	if err != nil {
		c.Errorf("Failed to read uploaded image: %v", err)
		http.Error(w, "Failed to get uploaded image", http.StatusBadRequest)
		return
	}
	ext := path.Ext(fileHeader.Filename)
	mimeType := mime.TypeByExtension(ext)

	// create context for Google Cloud Storage and upload file
	gc := gappengine.NewContext(r)
	hc := &http.Client{
		Transport: &oauth2.Transport{
			Source: google.AppEngineTokenSource(gc, storage.ScopeFullControl),
			Base:   &urlfetch.Transport{Context: gc},
		},
	}
	ctx := cloud.NewContext(gappengine.AppID(gc), hc)
	c.Infof("Demo GCS Application running from Version: %v\n", appengine.VersionID(c))

	filePath := path.Join(tempImagePath, createTempImagePath, fileHeader.Filename)
	c.Infof("file: %v, size: %d, MIME: %v, path: %v", fileHeader.Filename, len(data), mimeType, filePath)
	wc := storage.NewWriter(ctx, bucketName, filePath)
	wc.ContentType = mimeType
	_, err = wc.Write(data)
	if err != nil {
		c.Errorf("Failed to upload image: %v", err)
		http.Error(w, "Failed to upload image", http.StatusInternalServerError)
		return
	}
	err = wc.Close()
	if err != nil {
		c.Errorf("Failed to close uploaded image: %v", err)
		http.Error(w, "Failed to upload image", http.StatusInternalServerError)
		return
	}
	obj, err := storage.StatObject(ctx, bucketName, filePath)
	if err != nil {
		c.Errorf("Failed to stat object: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// c.Infof("obj: %v", obj)
	// get blob key for GCS file
	// obj := wc.Object()
	objName := path.Join("/gs", bucketName, obj.Name)
	c.Infof("Getting blob key from path: %v", objName)
	imgKey, err := blobstore.BlobKeyForFile(c, objName)
	if err != nil {
		c.Errorf("Failed to get image blob key: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	url, err := getImageUrl(c, imgKey)
	if err != nil {
		c.Errorf("Failed to get room image url: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	info := ImageInfo{
		Key: imgKey,
		Url: url.String(),
	}
	outBuf, err := json.Marshal(&info)
	if err != nil {
		c.Errorf("%s", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_, err = w.Write(outBuf)
	if err != nil {
		c.Errorf("%s", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}
