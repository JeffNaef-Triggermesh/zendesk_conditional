package main

import (
	"context"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/comprehend"
	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/kelseyhightower/envconfig"
	"go.uber.org/zap"
	"knative.dev/pkg/logging"
)

const (
	ticketCreatedEventType = "com.zendesk.ticket.created"
	tagCreateEventType     = "com.zendesk.tag.create"
	tagNegativeEventType   = "com.zendesk.tag.negative"
	eventSource            = "io.triggermesh.transformations.zendesk-sentiment-tag"

	transformTypeExtension = "transformtype"
)

type Receiver struct {
	client cloudevents.Client

	// If the K_SINK environment variable is set, then events are sent there,
	// otherwise we simply reply to the inbound request.
	Target string `envconfig:"K_SINK"`
	logger *zap.SugaredLogger
}

// Request is the structure of the event we expect to receive.
type Request struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}

// Response is the structure of the event we send in response to requests.
type Response struct {
	ID          int64  `json:"id"`
	Description string `json:"description"`
	Tag         string `json:"tag"`
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
func (recv *Receiver) handle(req Request) (Response, error) {

	id, err := strconv.Atoi(req.ID)
	if err != nil {
		recv.logger.Info("and error occured handling response: ")
		recv.logger.Error(err)
	}

	sentiment, err := recv.askComprehend(req.Description)
	if err != nil {
		recv.logger.Error(err)
		return Response{ID: 0}, err
	}

	return Response{ID: int64(id), Tag: sentiment, Description: req.Description}, nil
}

// receiveAndReply accepts a CloudEvent and responds to the sender (Because $K_SINK is not set)
func (recv *Receiver) receiveAndReply(ctx context.Context, event cloudevents.Event) (*cloudevents.Event, cloudevents.Result) {
	req := Request{}
	if err := event.DataAs(&req); err != nil {
		return nil, cloudevents.NewHTTPResult(400, "failed to convert data: %s", err)
	}

	recv.logger.Infof("Got an event from:")
	recv.logger.Infof(event.Source())
	resp, err := recv.handle(req)
	if err != nil {
		recv.logger.Error(err)
	}

	r := cloudevents.NewEvent(cloudevents.VersionV1)
	if resp.Tag == "NEGATIVE" {
		recv.logger.Info("Got a negative event:")
		r.SetType(tagNegativeEventType)
	} else {
		r.SetType(tagCreateEventType)
	}
	r.SetSubject("Zendesk Comprehend")
	r.SetSource(eventSource)
	r.SetTime(time.Now())
	recv.logger.Infof("Replying with event: ")
	recv.logger.Infof(r.String())
	if err := r.SetData("application/json", resp); err != nil {
		return nil, cloudevents.NewHTTPResult(500, "failed to set response data: %s", err)
	}
	return &r, nil
}

func (recv *Receiver) askComprehend(txt string) (string, error) {
	sess := session.Must(session.NewSessionWithOptions(session.Options{
		SharedConfigState: session.SharedConfigEnable,
	}))
	c := comprehend.New(sess)
	dSI := comprehend.DetectSentimentInput{}
	dSI.SetLanguageCode(os.Getenv("LANGUAGE"))
	dSI.SetText(txt)
	req, resp := c.DetectSentimentRequest(&dSI)
	if err := req.Send(); err != nil {
		recv.logger.Warn("Error Occured Requesting From AWS Comprehiend")
		recv.logger.Error(err)
		return "", err
	}

	recv.logger.Infof("Got A Response From Comprehiend: ")
	recv.logger.Infof(*resp.Sentiment)
	return *resp.Sentiment, nil
}

// OUTDATED AND NOT SUPPORTED SHOULD BE REMOVED BUT I IZ LAZY XD
// receiveAndSend Acceps CloudEvent's and sends a response to the Target defined on `K_SINK`
func (recv *Receiver) receiveAndSend(ctx context.Context, event cloudevents.Event) cloudevents.Result {
	req := Request{}
	if err := event.DataAs(&req); err != nil {
		return cloudevents.NewHTTPResult(400, "failed to convert data: %s", err)
	}

	recv.logger.Info("Got an event from:")
	recv.logger.Info(event.Source())
	recv.logger.Infof(event.String())
	resp, err := recv.handle(req)
	if err != nil {
		recv.logger.Error(err)
	}

	recv.logger.Infof("Sending event: %q", req.ID)
	recv.logger.Infof(event.String())
	newEvent := cloudevents.NewEvent(cloudevents.VersionV1)
	if resp.Tag == "NEGATIVE" {
		recv.logger.Info("Got a negative event:")
		event.SetType(tagNegativeEventType)
	} else {
		event.SetType(tagCreateEventType)
	}

	newEvent.SetSource("transformations.zenformation" + os.Getenv("NAMESPACE"))
	newEvent.SetSubject("Zendesk Comprehend")
	newEvent.SetTime(time.Now())
	if err := newEvent.SetData("application/json", resp); err != nil {
		return cloudevents.NewHTTPResult(500, "failed to set response data: %s", err)
	}

	ctx = cloudevents.ContextWithTarget(ctx, recv.Target)
	return recv.client.Send(ctx, newEvent)
}
