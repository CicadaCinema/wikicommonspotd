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
	"math"
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

type TweetPost struct {
	Data TweetPostInner `json:"data"`
}

type TweetPostInner struct {
	Id string `json:"id"`
}

type TweetRequestWithMedia struct {
	Text  string              `json:"text"`
	Media map[string][]string `json:"media"`
}

type TweetRequestInReply struct {
	Text  string            `json:"text"`
	Reply map[string]string `json:"reply"`
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

	// uploaded images must be at most 4096x4096 in size
	maxWidth := 4096
	if dimensions.Height > dimensions.Width {
		math.Floor((float64(4096) / float64(dimensions.Height)) * float64(dimensions.Width))
	}
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

	finalDimensions, err := bimg.NewImage(body).Size()
	if err != nil {
		log.WithError(err).Panic("could not get final image dimensions")
	}

	log.WithFields(log.Fields{"size": size, "width": finalDimensions.Width, "height": finalDimensions.Height}).Info("an acceptable result was obtained")

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
		log.WithError(err).Panic("could not decode Twitter API response and find id of uploaded media")
	}
	return strconv.Itoa(m.MediaId)
}

func checkValid(text string) bool {
	res, err := twtextparse.Parse(text)
	if err != nil {
		log.WithField("text", text).Panic("could not parse text to determine validity")
	}
	return res.IsValid
}

func tweetFromSlice(words []string) string {
	return strings.Join(words, " ")
}

func TruncateTweetBody(text string) []string {
	log.WithField("textInput", text).Info("starting to truncate text")

	const ellipsis = "..."

	var allTweets []string
	allWords := strings.Fields(text)
	previousAllWordsCount := len(allWords) + 1
	leadingEllipsis := ""
	for {
		// attempt to fit the entire slice of remaining words into a single tweet
		remainderString := leadingEllipsis + tweetFromSlice(allWords)

		// if this constitutes a valid tweet, then add this as a tweet and end the loop
		if checkValid(remainderString) {
			allTweets = append(allTweets, remainderString)
			log.WithField("tweet", remainderString).Info("generated final tweet")
			break
		}

		// make an assertion that the first word (with a trailing ellipsis, and a leading one if necessary) alone can fit in a tweet,
		// otherwise allWords can never decrease in size and an infinite loop will arise
		validTweet := leadingEllipsis + tweetFromSlice(allWords[:1]) + ellipsis
		if !checkValid(validTweet) {
			log.WithFields(log.Fields{"longWord": tweetFromSlice(allWords[:1]), "resultingTweet": validTweet}).Panic("word cannot fit into a tweet by itself")
		}

		var currentWords []string
		for {
			// if there are no more words left to add, then break
			if len(allWords) == 0 {
				break
			}

			// if this is the last word left of allWords, then we do not need a trailing ellipsis
			trailingEllipsis := ellipsis
			if len(allWords) == 1 {
				trailingEllipsis = ""
			}

			newWord := allWords[0]
			testTweet := leadingEllipsis + tweetFromSlice(append(currentWords, newWord)) + trailingEllipsis
			if checkValid(testTweet) {
				// if this is a valid tweet,
				// we can safely add newWord to the current tweet being constructed,
				// and remove the word we just added from allWords
				currentWords = append(currentWords, newWord)
				allWords = allWords[1:]
				// this test tweet was a valid one
				validTweet = testTweet
			} else {
				// otherwise, the addition of the new word would make the tweet invalid, so do not add it
				break
			}
		}

		allTweets = append(allTweets, validTweet)

		log.WithField("validTweet", validTweet).Info("generated one more valid tweet")

		// a leading ellipsis will now be necessary for all subsequent tweets (all but the first one)
		leadingEllipsis = ellipsis

		// if the length of allWords does not decrease with each iteration of the loop, something has gone wrong
		if len(allWords) >= previousAllWordsCount {
			log.WithFields(log.Fields{"allWords": allWords, "previousAllWordsCount": previousAllWordsCount, "allTweets": allTweets, "validTweet": validTweet}).Panic("something has gone wrong and the length of allWords has not decreased during this iteration")
		}
	}

	log.WithField("allTweets", allTweets).Info("finished generating tweets")

	return allTweets
}

func postTweetWithImage(httpClient *http.Client, tweetBody string, mediaId string) string {
	// create an object to be used in the http POST request to twitter
	req := TweetRequestWithMedia{
		Text: tweetBody,
		Media: map[string][]string{
			"media_ids": {mediaId},
		},
	}

	postBody, err := json.Marshal(req)
	if err != nil {
		log.WithError(err).WithFields(log.Fields{"text": tweetBody, "mediaId": mediaId}).Panic("could not marshal TweetRequestWithMedia object to JSON")
	}

	log.WithField("requestBody", string(postBody)).Info("post body generated")

	resp, err := httpClient.Post("https://api.twitter.com/2/tweets", "application/json", bytes.NewBuffer(postBody))

	// handle error
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

	// read media id from Twitter API response
	t := &TweetPost{}
	err = json.NewDecoder(resp.Body).Decode(t)
	if err != nil {
		log.WithError(err).Panic("could not decode Twitter API response and find id of posted tweet")
	}
	return t.Data.Id
}

func postTweetInReply(httpClient *http.Client, tweetBody string, replyId string) string {
	// create an object to be used in the http POST request to twitter
	req := TweetRequestInReply{
		Text: tweetBody,
		Reply: map[string]string{
			"in_reply_to_tweet_id": replyId,
		},
	}

	postBody, err := json.Marshal(req)
	if err != nil {
		log.WithError(err).WithFields(log.Fields{"text": tweetBody, "replyId": replyId}).Panic("could not marshal TweetRequestInReply object to JSON")
	}

	log.WithField("requestBody", string(postBody)).Info("post body generated for tweet")

	resp, err := httpClient.Post("https://api.twitter.com/2/tweets", "application/json", bytes.NewBuffer(postBody))

	// handle error
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

	// read media id from Twitter API response
	t := &TweetPost{}
	err = json.NewDecoder(resp.Body).Decode(t)
	if err != nil {
		log.WithError(err).Panic("could not decode Twitter API response and find id of posted tweet")
	}
	return t.Data.Id
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

	// generate batch of tweets to send out
	tweetsBatch := TruncateTweetBody(potd.Description)

	if len(tweetsBatch) >= 100 {
		log.WithFields(log.Fields{"description": potd.Description, "tweetCount": len(tweetsBatch), "tweets": tweetsBatch}).Panic("too many tweets generated from description")
	}

	// post initial tweet with image
	id := postTweetWithImage(httpClient, tweetsBatch[0], mediaId)
	log.WithField("id", id).Info("tweet posted with media")
	tweetsBatch = tweetsBatch[1:]

	// post each of the remaining tweets
	for _, tweetText := range tweetsBatch {
		id = postTweetInReply(httpClient, tweetText, id)
		log.WithField("id", id).Info("tweet posted in reply to previous tweet")
	}

	log.Info("done posting tweets")
}
