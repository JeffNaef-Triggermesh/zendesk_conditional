package main

import (
	"context"
	"log"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/kelseyhightower/envconfig"
	"go.uber.org/zap"
	"knative.dev/pkg/logging"
)

// Receiver quiet linter!
type Receiver struct {
	client cloudevents.Client

	// If the K_SINK environment variable is set, then events are sent there,
	// otherwise we simply reply to the inbound request.
	Target string `envconfig:"K_SINK"`
	logger *zap.SugaredLogger
	Bucket string `envconfig:"BUCKET"`
}

// Request is the structure of the event we expect to receive.
type Request struct {
	ID          int64  `json:"id"`
	Description string `json:"description"`
	Tag         string `json:"tag"`
}

// Response is the structure of the event we send in response to requests.
type Response struct {
	ID          int64  `json:"id"`
	Tag         string `json:"tag"`
	Description string `json:"description"`
}

func main() {
	ctx := context.Background()
	r := Receiver{}
	r.logger = logging.FromContext(ctx)
	var er error
	r.client, er = cloudevents.NewDefaultClient()
	if er != nil {
		r.logger.Fatal(er)
	}

	if err := envconfig.Process("", &r); err != nil {
		log.Fatal(err)
	}

	r.logger.Info("FeEd Me MoRe eVeNtS!")

	// Depending on whether targeting data has been supplied,
	// we will either reply with our response or send it on to
	// an event sink.
	var receiver interface{}
	if r.Target == "" {
		receiver = r.receiveAndReply
	} else {
		receiver = r.receiveAndSend
	}

	if err := r.client.StartReceiver(ctx, receiver); err != nil {
		log.Fatal(err)
	}

}

// handle shared the logic for producing the Response event from the Request.
func (recv *Receiver) handle(req Request, event cloudevents.Event) error {

	err := recv.s3ification(req.Description, event)
	if err != nil {
		recv.logger.Errorw("and error occured posting to s3 ", zap.Error(err))
		return err
	}

	return nil
}

// Transformation creates an interface for this package so it can be re-used to create others
type Transformation interface {
	Start(ctx context.Context) error
}

// receiveAndSend Acceps CloudEvent's and sends a response to the Target defined on `K_SINK`
func (recv *Receiver) receiveAndSend(ctx context.Context, event cloudevents.Event) cloudevents.Result {
	req := Request{}
	if err := event.DataAs(&req); err != nil {
		return cloudevents.NewHTTPResult(400, "failed to convert data: %s", err)
	}

	recv.logger.Info("Got an event from:")
	recv.logger.Info(event.Source())
	err := recv.handle(req, event)
	if err != nil {
		recv.logger.Errorw("and error occured handling the event: ", zap.Error(err))
	}

	log.Printf("Sending event: %q", req.ID)
	newEvent := cloudevents.NewEvent(cloudevents.VersionV1)
	newEvent.SetType("com.zendesk.tag.create")
	newEvent.SetSource("transformations.conditionalization." + os.Getenv("NAMESPACE"))
	newEvent.SetSubject("Zendesk Comprehend")
	newEvent.SetTime(time.Now())
	if err := newEvent.SetData("application/json", Response{ID: req.ID, Tag: req.Tag}); err != nil {
		return cloudevents.NewHTTPResult(500, "failed to set response data: %s", err)
	}

	ctx = cloudevents.ContextWithTarget(ctx, recv.Target)
	return recv.client.Send(ctx, newEvent)
}

// receiveAndReply accepts a CloudEvent and responds to the sender (Because $K_SINK is not set)
func (recv *Receiver) receiveAndReply(ctx context.Context, event cloudevents.Event) (*cloudevents.Event, cloudevents.Result) {
	req := Request{}
	if err := event.DataAs(&req); err != nil {
		return nil, cloudevents.NewHTTPResult(400, "failed to convert data: %s", err)
	}

	recv.logger.Info("Got an event from:")
	recv.logger.Info(event.Source())
	err := recv.handle(req, event)
	if err != nil {
		recv.logger.Errorw("and error occured handling the event: ", zap.Error(err))
	}

	recv.logger.Infof("Replying with event: ")
	newEvent := cloudevents.NewEvent(cloudevents.VersionV1)
	newEvent.SetType("com.zendesk.tag.create")
	newEvent.SetSource("transformations.conditionalization." + os.Getenv("NAMESPACE"))
	newEvent.SetSubject("Zendesk Comprehend")
	newEvent.SetTime(time.Now())
	if err := newEvent.SetData("application/json", Response{ID: req.ID, Tag: req.Tag}); err != nil {
		return nil, cloudevents.NewHTTPResult(500, "failed to set response data: %s", err)
	}

	return &newEvent, nil
}

func (recv *Receiver) s3ification(txt string, event cloudevents.Event) error {

	bucket := recv.Bucket
	filename := event.Source() + ".txt"

	// create a reader from data data in memory
	reader := strings.NewReader(txt)

	sess, err := session.NewSession()
	uploader := s3manager.NewUploader(sess)

	_, err = uploader.Upload(&s3manager.UploadInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(filename),
		Body:   reader,
	})

	if err != nil {
		recv.logger.Errorw("Unable to upload.", zap.Error(err))
		return err
	}

	recv.logger.Infof("Successfully uploaded %q to %q\n", filename, bucket)

	return nil
}
