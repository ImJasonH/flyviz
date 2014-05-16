package app

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"appengine"
	"appengine/urlfetch"
)

func init() {
	http.HandleFunc("/upload", uploadHandler)
}

const apiKey = "AIzaSyAyhQ8SoM1psusUXChfqle92RWYasvmEEc"

// RoundTripper that adds a token before making the request.
type tokenRT struct{ c appengine.Context }

func (t tokenRT) RoundTrip(r *http.Request) (*http.Response, error) {
	tok, _, err := appengine.AccessToken(t.c, "https://www.googleapis.com/auth/drive.file")
	if err != nil {
		return nil, err
	}
	u := r.URL.Query()
	u.Set("key", apiKey)
	r.URL.RawQuery = u.Encode()
	r.Header.Set("Authorization", "Bearer "+tok)
	return urlfetch.Client(t.c).Do(r)
}

func uploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, r.Method+" not supported", http.StatusMethodNotAllowed)
		return
	}

	c := appengine.NewContext(r)
	client := http.Client{
		Transport: tokenRT{c},
	}

	f := r.FormValue("file")

	// Upload file and request conversion
	iresp, err := client.Post("https://www.googleapis.com/upload/drive/v2/files?uploadType=media&convert=true", "application/vnd.ms-excel", strings.NewReader(f))
	if err != nil {
		c.Errorf("%v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer iresp.Body.Close()
	if iresp.StatusCode != http.StatusOK {
		var buf bytes.Buffer
		io.Copy(&buf, iresp.Body)
		c.Errorf(buf.String())
		c.Errorf("Error %d", iresp.StatusCode)
		http.Error(w, buf.String(), http.StatusInternalServerError)
		return
	}

	// Get the link to export to CSV
	var insertResp struct {
		ID          string
		ExportLinks map[string]string
	}
	if err := json.NewDecoder(iresp.Body).Decode(&insertResp); err != nil {
		c.Errorf("%v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	link := insertResp.ExportLinks["application/pdf"]
	if link == "" {
		c.Errorf("couldn't get export link")
		http.Error(w, "no export link", http.StatusInternalServerError)
		return
	}
	link = strings.Replace(link, "=pdf", "=csv", 1)

	// Fetch export link
	eresp, err := client.Get(link)
	if err != nil {
		c.Errorf("%v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer eresp.Body.Close()
	if eresp.StatusCode != http.StatusOK {
		var buf bytes.Buffer
		io.Copy(&buf, eresp.Body)
		c.Errorf(buf.String())
		c.Errorf("Error %d", eresp.StatusCode)
		http.Error(w, buf.String(), http.StatusInternalServerError)
		return
	}
	io.Copy(w, eresp.Body) // TODO: Moar better.

	// Trash temporary file uploaded for conversion
	if _, err := client.Post("https://www.googleapis.com/drive/v2/files/"+insertResp.ID+"/trash", "application/json", nil); err != nil {
		c.Infof("failed to trash: %v", err)
	}
}
