package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/dghubble/oauth1"
	log "github.com/sirupsen/logrus"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"os"
	"strconv"
	"strings"
)

type PotdEntry struct {
	Description string `json:"description"`
	DownloadUrl string `json:"downloadUrl"`
}

type MediaUpload struct {
	MediaId int `json:"media_id"`
}

type TweetRequest struct {
	Text  string              `json:"text"`
	Media map[string][]string `json:"media"`
}

func getPotdInfo(spreadsheetId string, sheetRange string) PotdEntry {
	ctx := context.Background()

	// create sheets service to access the API
	// TODO: store this in an environment variable
	srv, err := sheets.NewService(ctx, option.WithAPIKey("AIzaSyCbiT9ks0PJZCgy1Fi56sL6-5fFlVaqV68"))
	if err != nil {
		log.WithError(err).Panic("unable to retrieve sheets service")
	}

	// fetch data from the given range of the given sheet
	resp, err := srv.Spreadsheets.Values.Get(spreadsheetId, sheetRange).Do()
	if err != nil {
		log.WithError(err).Panic("unable to retrieve data from sheet")
	}

	if len(resp.Values) == 0 {
		log.WithError(err).Panic("no data found in sheet")
	}
	row := resp.Values[0]

	// isolate description text
	descriptionCut := strings.Split(fmt.Sprintf("%s", row[0]), "] \n\n")[1]
	descriptionCleaned := strings.Replace(descriptionCut, "\n", "", -1)

	// return two formatted strings representing the potd entry for today
	return PotdEntry{
		Description: descriptionCleaned,
		DownloadUrl: fmt.Sprintf("%s", row[1]),
	}
}

func downloadFile(file *os.File, url string) {
	// get the potd image via http
	resp, err := http.Get(url)
	if err != nil {
		log.WithError(err).Panic("could not download potd image via http")
	}
	defer resp.Body.Close()

	// check http response
	if resp.StatusCode != http.StatusOK {
		log.WithFields(log.Fields{"statusCode": resp.StatusCode, "body": resp.Body}).Panic("bad http status while downloading potd image")
	}

	// write the download response body to the provided file
	_, err = io.Copy(file, resp.Body)
	if err != nil {
		log.WithError(err).WithField("path", file.Name()).Panic("could not write potd image to file")
	}
}

func getAuthorisedClient() *http.Client {
	// authorize the developer account @WikiCommonsPOTD
	// TODO: store this in an environment variable
	config := oauth1.NewConfig("dVeflYuXw41yARdDiyx3h4hWU", "mGKCM7JEWdGNJ1CO5BcnjLmkqxTc9cPBJEehxvKXx6tI2XCvTC")
	token := oauth1.NewToken("1545826323569967107-iu5Pl20y1h7Ei3HJPzoWKBqA2uUh27", "XaDRGgDo6NQTWwvqOuZSsDgDtnR0jiGC5rIwvvaXLU0YI")

	return config.Client(oauth1.NoContext, token)
}

func uploadImage(httpClient *http.Client, imagePath string) string {
	// create body form
	b := &bytes.Buffer{}
	form := multipart.NewWriter(b)

	// create media parameter
	fw, err := form.CreateFormFile("media", imagePath)
	if err != nil {
		log.WithError(err).Panic("could not create media parameter")
	}

	data, err := os.Open(imagePath)
	if err != nil {
		log.WithError(err).WithField("path", imagePath).Panic("could not open potd image file")
	}

	// copy to form
	_, err = io.Copy(fw, data)
	if err != nil {
		log.WithError(err).Panic("could not copy potd image data to form")
	}

	// close form
	err = form.Close()
	if err != nil {
		log.WithError(err).Panic("could not close form")
	}

	// upload media
	resp, err := httpClient.Post("https://upload.twitter.com/1.1/media/upload.json?media_category=tweet_image", form.FormDataContentType(), bytes.NewReader(b.Bytes()))
	if err != nil {
		log.WithError(err).Panic("could not upload media to Twitter")
	}
	defer resp.Body.Close()

	// check http response
	if resp.StatusCode != http.StatusOK {
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			log.WithError(err).WithField("statusCode", resp.StatusCode).Panic("could not read http response body after attempt to upload media to Twitter resulted in a bad http status")
		}
		log.WithFields(log.Fields{"statusCode": resp.StatusCode, "body": string(body)}).Panic("bad http status while uploading media to Twitter")
	} else {
		log.Info("received http OK on uploading media")
	}

	// read media id from Twitter API response
	m := &MediaUpload{}
	err = json.NewDecoder(resp.Body).Decode(m)
	if err != nil {
		log.WithError(err).Panic("could not decode Twitter API response")
	}
	return strconv.Itoa(m.MediaId)
}

func postTweetWithImage(httpClient *http.Client, tweetBody string, mediaId string) {
	// create an object to be used in the http POST request to twitter
	req := TweetRequest{
		Text: tweetBody,
		Media: map[string][]string{
			"media_ids": {mediaId},
		},
	}

	postBody, err := json.Marshal(req)
	if err != nil {
		log.WithError(err).WithFields(log.Fields{"text": tweetBody, "mediaId": mediaId}).Panic("could not marshal TweetRequest object to JSON")
	}

	fmt.Println(string(postBody))

	resp, err := httpClient.Post("https://api.twitter.com/2/tweets", "application/json", bytes.NewBuffer(postBody))

	//Handle Error
	if err != nil {
		log.WithError(err).Panic("could not submit tweet")
	}
	defer resp.Body.Close()

	// check http response
	if resp.StatusCode != http.StatusOK {
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			log.WithError(err).WithField("statusCode", resp.StatusCode).Panic("could not read http response body after attempt to submit tweet resulted in a bad http status")
		}
		log.WithFields(log.Fields{"statusCode": resp.StatusCode, "body": string(body)}).Panic("bad http status while submitting tweet")
	} else {
		log.Info("received http OK on submitting tweet")
	}
}

func main() {
	// set logging options
	log.SetLevel(log.DebugLevel)
	log.SetFormatter(&log.JSONFormatter{})
	log.Info("logger started")

	// fetch today's potd data from Google Sheets
	potd := getPotdInfo("1Dt5PGEW5p5biif8QGuOfYTHjH8NeRfkf2vaHxuqoYGc", "Sheet1!Y1:Z1")
	log.WithField("PotdEntry", potd).Info("fetched today's potd")

	// create unique temporary file which will be overwritten by the potd image
	tempFile, err := os.CreateTemp("", "potdFile")
	if err != nil {
		log.WithError(err).Panic("failed to create temporary file")
	}
	defer tempFile.Close()
	defer os.Remove(tempFile.Name())
	log.WithField("path", tempFile.Name()).Info("created temporary file")

	// download the potd image, saving it to the path of the temporary file
	downloadFile(tempFile, potd.DownloadUrl)
	log.WithFields(log.Fields{
		"url":         potd.DownloadUrl,
		"destination": tempFile.Name(),
	}).Info("downloaded potd image")

	// TODO: resize image to fit Twitter's 5MB limit before uploading

	// this Client will automatically authorize any requests to the Twitter API
	httpClient := getAuthorisedClient()
	log.Info("created http client")

	mediaId := uploadImage(httpClient, tempFile.Name())
	log.Info("potd image uploaded")

	postTweetWithImage(httpClient, potd.Description, mediaId)
	log.Info("tweet posted")
}
