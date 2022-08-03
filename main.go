package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/dghubble/oauth1"
	"github.com/h2non/bimg"
	twtextparse "github.com/myl7/twitter-text-parse-go/pkg/gnu"
	log "github.com/sirupsen/logrus"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
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

func compressFile(path string, quality int, fileSizeLimit int) string {
	log.WithFields(log.Fields{
		"fileSizeLimit": fileSizeLimit,
		"jpegQuality":   quality,
	}).Info("starting compression algorithm")

	originalBuffer, err := bimg.Read(path)
	if err != nil {
		log.WithError(err).WithField("path", path).Panic("could not read input file to buffer")
	}
	size := len(originalBuffer)
	log.WithField("size", size).Info("read the size of the original file")

	// if the size of the file is already below Twitter's limit, just return its path
	if size < fileSizeLimit {
		log.Info("no image processing needed, file size is already below limit")
		return path
	}

	dimensions, err := bimg.NewImage(originalBuffer).Size()
	if err != nil {
		log.WithError(err).Panic("could not get image dimensions")
	}

	maxWidth := dimensions.Width
	minWidth := 1
	var body []byte

	// use binary search to find the highest resolution giving an acceptable file size
	log.Info("starting binary search algorithm")
	for maxWidth-minWidth >= 10 {
		log.WithFields(log.Fields{"maxWidth": maxWidth, "minWidth": minWidth}).Info("unacceptable range, retrying")
		testWidth := (maxWidth + minWidth) / 2
		body, err = bimg.NewImage(originalBuffer).Process(bimg.Options{Width: testWidth, Quality: quality, Type: bimg.JPEG})
		if err != nil {
			log.WithError(err).WithField("width", testWidth).Panic("failed to execute re-encode operation")
		}
		size = len(body)
		if size >= fileSizeLimit {
			maxWidth = testWidth
		} else {
			minWidth = testWidth
		}
	}

	log.WithFields(log.Fields{"size": size, "width": minWidth}).Info("an acceptable result was obtained")

	err = bimg.Write("new.jpeg", body)
	if err != nil {
		log.WithError(err).Panic("could not write new image to disk")
	}

	cwd, err := os.Getwd()
	if err != nil {
		log.WithError(err).Panic("could not get current working directory")
	}

	return filepath.Join(cwd, "new.jpeg")
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
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
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

func TruncateTweetBody(text string) string {
	log.WithField("textInput", text).Info("starting to truncate text")

	// test if tweet body is valid without modification
	res, err := twtextparse.Parse(text)
	if err != nil {
		log.WithField("text", text).Panic("could not parse unmodified tweet text")
	}

	// initialise variables, ensuring textFinal is the unmodified string (it will be returned if the unmodified result is valid)
	words := strings.Fields("")
	wordsForTest := append(words, "...")
	textFinal := text

	valid := res.IsValid
	// if the result is still invalid,
	for !valid {
		// split the input string into words, remove the last word and concatenate the slice into a string again for the next iteration
		words = strings.Fields(text)
		words = words[:len(words)-1]
		text = strings.Join(words, " ")

		// add an ellipsis and concatenate the slice into a string for testing
		wordsForTest = append(words, "...")
		textFinal = strings.Join(wordsForTest, " ")

		// test if textFinal is valid
		res, err = twtextparse.Parse(textFinal)
		if err != nil {
			log.WithField("text", textFinal).Panic("could not parse tweet text")
		}
		valid = res.IsValid

	}

	log.WithField("textOutput", textFinal).Info("finished truncating text")
	return textFinal
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

	log.WithField("requestBody", string(postBody)).Info("post body generated")

	resp, err := httpClient.Post("https://api.twitter.com/2/tweets", "application/json", bytes.NewBuffer(postBody))

	//Handle Error
	if err != nil {
		log.WithError(err).Panic("could not submit tweet")
	}
	defer resp.Body.Close()

	// check http response
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
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
	defer os.Remove(tempFile.Name())
	log.WithField("path", tempFile.Name()).Info("created temporary file")

	// download the potd image, saving it to the path of the temporary file
	downloadFile(tempFile, potd.DownloadUrl)
	log.WithFields(log.Fields{
		"url":         potd.DownloadUrl,
		"destination": tempFile.Name(),
	}).Info("downloaded potd image")

	// close the temporary file
	err = tempFile.Close()
	if err != nil {
		log.WithError(err).Panic("could not close potd image file")
	}

	// resize image to fit Twitter's 5MB limit before uploading
	compressedFile := compressFile(tempFile.Name(), 90, 5000000)
	defer os.Remove(compressedFile)

	// this Client will automatically authorize any requests to the Twitter API
	httpClient := getAuthorisedClient()
	log.Info("created http client")

	mediaId := uploadImage(httpClient, compressedFile)
	log.Info("potd image uploaded")

	postTweetWithImage(httpClient, TruncateTweetBody(potd.Description), mediaId)
	log.Info("tweet posted")
}
