package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"strings"

	"appengine"
	"appengine/urlfetch"

	"code.google.com/p/goauth2/oauth"
	value "gist.github.com/2dff7bd89dcacd3e70b9.git"
	"github.com/ImJasonH/csvstruct"
)

func init() {
	http.HandleFunc("/upload", uploadHandler)
}

var tmpl = template.Must(template.ParseFiles("app.tmpl"))

func uploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, r.Method+" not supported", http.StatusMethodNotAllowed)
		return
	}
	c := appengine.NewContext(r)

	// Set up the HTTP client using urlfetch and OAuth creds
	clientID := value.Get(c, "client_id")
	secret := value.Get(c, "client_secret")
	refresh := value.Get(c, "refresh_token")
	trans := oauth.Transport{
		Config: &oauth.Config{
			ClientId:     clientID,
			ClientSecret: secret,
			TokenURL:     "https://accounts.google.com/o/oauth2/token",
		},
		Token:     &oauth.Token{RefreshToken: refresh},
		Transport: &urlfetch.Transport{Context: c},
	}
	client := trans.Client()

	// Upload file and request conversion
	f := r.FormValue("file")
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

	// Trash temporary file uploaded for conversion
	if _, err := client.Post("https://www.googleapis.com/drive/v2/files/"+insertResp.ID+"/trash", "application/json", nil); err != nil {
		c.Infof("failed to trash: %v", err)
	}

	// Decode CSV and print
	type class struct {
		Date                  int64
		Time                  string
		Classroom             string
		Instructor            string
		AvgRPM                int     `csv:"Avg RPM"`
		MaxRPM                int     `csv:"Max RPM"`
		AvgTorq               int     `csv:"Avg Torq"`
		MaxTorq               int     `csv:"Max Torq"`
		AvgSpeed              int     `csv:"Avg Speed"`
		ClassTime             float64 `csv:"Class Time (TODO)"`
		TotalPower            int     `csv:"Total Power"`
		TotalDistance         int     `csv:"Total Distance"`
		EstimatedCaloriesLow  int     `csv:"Estimated Calories Low"`
		EstimatedCaloriesHigh int     `csv:"Estimated Calories High"`
	}
	d := csvstruct.NewDecoder(eresp.Body)
	classes := []class{}
	var cls class
	for err != io.EOF {
		if err = d.DecodeNext(&cls); err != nil && err != io.EOF {
			classes = append(classes, cls)
		}
	}
	fmt.Fprintf(w, "%v", classes)
}
