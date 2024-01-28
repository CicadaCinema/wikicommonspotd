package main

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/dghubble/oauth1"
	"github.com/h2non/bimg"
	twtextparse "github.com/myl7/twitter-text-parse-go/pkg/gnu"
	log "github.com/sirupsen/logrus"
	"golang.org/x/net/html"
)

type PotdEntry struct {
	Description string
	DownloadUrl string
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

func assert(condition bool, message string) {
	if !condition {
		log.Panic(message)
	}
}

func depthFirstTraverse(node *html.Node, visit func(*html.Node)) {
	// boilerplate copied from https://pkg.go.dev/golang.org/x/net/html
	var f func(*html.Node)
	f = func(n *html.Node) {
		visit(n)
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(node)
}

func textDescription(node *html.Node) string {
	result := ""
	depthFirstTraverse(node, func(n *html.Node) {
		if n.Type == html.TextNode {
			result += n.Data
		}
	})
	return result
}

func getPotdFromXML(htmlTable string) PotdEntry {
	doc, err := html.Parse(strings.NewReader(htmlTable))
	if err != nil {
		log.WithError(err).Panic("unable to parse html table")
	}

	// attempt to find the descriptions node and url
	descriptions := []string{}
	var fileName, thumbnailUrl string
	var foundFileName, foundThumbnailUrl bool
	depthFirstTraverse(doc, func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "div" {
			for _, attr := range n.Attr {
				if attr.Key == "class" && slices.Contains(strings.Split(attr.Val, " "), "description") {
					// found a description node
					descriptions = append(descriptions, textDescription(n))
					break
				}
			}
		}
		if n.Type == html.ElementNode && n.Data == "a" {
		outer1:
			for _, attr1 := range n.Attr {
				if attr1.Key == "class" && slices.Contains(strings.Split(attr1.Val, " "), "mw-file-description") {
					for _, attr2 := range n.Attr {
						if attr2.Key == "href" {
							// found a node with the filename
							if foundFileName {
								log.Warn("expected one filename, found multiple")
							} else {
								fileName = attr2.Val[11:]
								foundFileName = true
							}
							break outer1
						}

					}
				}

			}
		}
		if n.Type == html.ElementNode && n.Data == "img" {
		outer2:
			for _, attr1 := range n.Attr {
				if attr1.Key == "class" && slices.Contains(strings.Split(attr1.Val, " "), "mw-file-element") {
					for _, attr2 := range n.Attr {
						if attr2.Key == "src" {
							// found a node with the thumbnail url
							if foundThumbnailUrl {
								log.Warn("expected one thumbnail url, found multiple")
							} else {
								thumbnailUrl = attr2.Val
								foundThumbnailUrl = true
							}
							break outer2
						}

					}
				}

			}
		}

	})

	// we only expect one description, but this is non-critical because it does not effect the tweet being posted
	if len(descriptions) > 1 {
		log.WithField("descriptions", descriptions).Warn("expected one description, parsed html to find multiple")
	} else if len(descriptions) == 0 {
		log.WithField("descriptions", descriptions).Warn("expected one description, parsed html to find zero")

		// insert zero value
		descriptions = append(descriptions, "")
	}

	// expect to find both the filename and the thumbnail url
	if !foundFileName {
		log.Warn("expected to find filename")
	}
	if !foundThumbnailUrl {
		log.Warn("expected to find thumbnail url")
	}

	// only attempt to construct download url if we have found both of the above
	downloadUrl := ""
	if foundFileName && foundThumbnailUrl {
		// bring both strings to a consistent form
		thumbnailUrlUnescaped, err := url.QueryUnescape(thumbnailUrl)
		if err != nil {
			log.WithFields(log.Fields{"thumbnailUrl": thumbnailUrl, "thumbnailUrlUnescaped": thumbnailUrlUnescaped}).Warn("failed to URL unescape thumbnail URL")
		} else {
			thumbnailUrl = thumbnailUrlUnescaped
		}
		fileNameUnescaped, err := url.QueryUnescape(fileName)
		if err != nil {
			log.WithFields(log.Fields{"fileName": fileName, "fileNameUnescaped": fileNameUnescaped}).Warn("failed to URL unescape filename")
		} else {
			fileName = fileNameUnescaped
		}

		log.WithFields(log.Fields{"fileName": fileName, "thumbnailUrl": thumbnailUrl}).Info("found filename and thumbnail URL")

		thumbnailParts := strings.Split(thumbnailUrl, fileName)
		downloadUrl = strings.Replace(thumbnailParts[0], "/thumb", "", 1) + fileName
	}

	return PotdEntry{Description: descriptions[0], DownloadUrl: downloadUrl}
}

func getHtmlFromFeed() string {
	// request feed via http
	resp, err := http.Get("https://commons.wikimedia.org/w/api.php?action=featuredfeed&feed=potd&language=en")
	if err != nil {
		log.WithError(err).Panic("unable to retrieve RSS feed via http")
	}
	defer resp.Body.Close()

	// retrieve serialised xml from body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.WithError(err).WithField("statusCode", resp.StatusCode).Panic("unable to read http response body after retrieving RSS feed")
	}

	// unmarshal the only field we need
	type FeedXML struct {
		HtmlTable string `xml:"channel>item>description"`
	}
	var feedXml FeedXML
	err = xml.Unmarshal(body, &feedXml)
	if err != nil {
		log.WithError(err).Panic("unable to unmarshal RSS XML feed")
	}

	return feedXml.HtmlTable
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
	type Configuration struct {
		ApiKey            string
		ApiKeySecret      string
		AccessToken       string
		AccessTokenSecret string
	}

	confFile, err := os.Open("conf.json")
	if err != nil {
		log.WithError(err).Panic("unable to open configuration file")
	}
	defer confFile.Close()

	var conf Configuration
	err = json.NewDecoder(confFile).Decode(&conf)
	if err != nil {
		log.WithError(err).Panic("unable to decode configuration file")
	}

	// API Key and API Key Secret
	config := oauth1.NewConfig(conf.ApiKey, conf.ApiKeySecret)
	// Access Token and Access Token Secret
	token := oauth1.NewToken(conf.AccessToken, conf.AccessTokenSecret)

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
		body, err := io.ReadAll(resp.Body)
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
		body, err := io.ReadAll(resp.Body)
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
		body, err := io.ReadAll(resp.Body)
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

	// fetch today's potd data from RSS Feed
	potd := getPotdFromXML(getHtmlFromFeed())
	log.WithField("potdEntry", potd).Info("fetched today's potd")

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
