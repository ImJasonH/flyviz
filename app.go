package app

import (
	"bytes"
	"encoding/json"
	"html/template"
	"io"
	"net/http"
	"strings"

	"appengine"
	"appengine/urlfetch"

	"code.google.com/p/goauth2/oauth"
	value "github.com/ImJasonH/appengine-value"
	"github.com/ImJasonH/csvstruct"
)

func init() {
	http.HandleFunc("/upload", uploadHandler)
}

var tmpl = template.Must(template.ParseFiles("app.tmpl"))

func uploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
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
	// Explicitly exercise the refresh token to get an access token
	if err := trans.Refresh(); err != nil {
		c.Errorf("%v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	client := trans.Client()

	// Upload file and request conversion
	f, _, err := r.FormFile("file")
	if err != nil {
		c.Errorf("%v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer f.Close()
	iresp, err := client.Post("https://www.googleapis.com/upload/drive/v2/files?uploadType=media&convert=true", "application/vnd.ms-excel", f)
	if err != nil {
		c.Errorf("uploading for conversion: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer iresp.Body.Close()
	if iresp.StatusCode != http.StatusOK {
		var buf bytes.Buffer
		io.Copy(&buf, iresp.Body)
		c.Errorf(buf.String())
		c.Errorf("uploading for conversion: error %d", iresp.StatusCode)
		http.Error(w, buf.String(), http.StatusInternalServerError)
		return
	}

	// Get the link to export to CSV
	var insertResp struct {
		ID          string
		ExportLinks map[string]string
	}
	if err := json.NewDecoder(iresp.Body).Decode(&insertResp); err != nil {
		c.Errorf("decoding json: %v", err)
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
		c.Errorf("fetching csv: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer eresp.Body.Close()
	if eresp.StatusCode != http.StatusOK {
		var buf bytes.Buffer
		io.Copy(&buf, eresp.Body)
		c.Errorf(buf.String())
		c.Errorf("error fetching csv %d", eresp.StatusCode)
		http.Error(w, buf.String(), http.StatusInternalServerError)
		return
	}

	// Trash temporary file uploaded for conversion
	if _, err := client.Post("https://www.googleapis.com/drive/v2/files/"+insertResp.ID+"/trash", "application/json", nil); err != nil {
		c.Infof("failed to trash: %v", err)
	}

	// Decode CSV and print
	type class struct {
		Date                  int64 `csv:"-"`
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
	for {
		if err := d.DecodeNext(&cls); err == io.EOF {
			break
		} else if err != nil {
			c.Errorf("%v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		classes = append(classes, cls)
	}
	if err := tmpl.Execute(w, classes); err != nil {
		c.Warningf("%v", err)
	}
}
