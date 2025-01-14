package influxdbfirehosenozzle

import (
	"crypto/tls"
	"time"
	"strings"

	"github.com/cloudfoundry/noaa/consumer"
	noaaerrors "github.com/cloudfoundry/noaa/errors"
        "github.com/cloudfoundry/gosteno"
	"github.com/cloudfoundry/sonde-go/events"
	"github.com/MichaelSp/influxdb-firehose-nozzle/influxdbclient"
	"github.com/MichaelSp/influxdb-firehose-nozzle/nozzleconfig"
	"github.com/gorilla/websocket"
	"code.cloudfoundry.org/localip"
	"github.com/MichaelSp/influxdb-firehose-nozzle/cfinstanceinfoapi"
)

type InfluxDbFirehoseNozzle struct {
	config           *nozzleconfig.NozzleConfig
        errs             <-chan error
        messages         <-chan *events.Envelope
	authToken        string
        consumer         *consumer.Consumer
	client           *influxdbclient.Client
        log              *gosteno.Logger
	appinfo          map[string]cfinstanceinfoapi.AppInfo
}

func NewInfluxDbFirehoseNozzle(config *nozzleconfig.NozzleConfig, authToken string, log *gosteno.Logger, appinfo map[string]cfinstanceinfoapi.AppInfo) *InfluxDbFirehoseNozzle {
	return &InfluxDbFirehoseNozzle{
		config:           config,
		authToken:        authToken,
                log:              log,
		appinfo:          appinfo,
	}
}

func (d *InfluxDbFirehoseNozzle) Start() error {
	var authToken string

	if !d.config.DisableAccessControl {
		authToken = d.authToken
	}

	d.log.Info("Starting InfluxDb Firehose Nozzle...")
	d.createClient()
	d.consumeFirehose(authToken)
	err := d.postToInfluxDb()
	d.log.Info("InfluxDb Firehose Nozzle shutting down...")
	return err
}

func (d *InfluxDbFirehoseNozzle) createClient() {
	ipAddress, err := localip.LocalIP()
	if err != nil {
		panic(err)
	}

	d.client = influxdbclient.New(
		d.config.InfluxDbUrl,
 		d.config.InfluxDbDatabase,
 		d.config.InfluxDbRP,
		d.config.InfluxDbUser,
		d.config.InfluxDbPassword,
		d.config.InfluxDbSslSkipVerify,
 		d.config.MetricPrefix,
 		d.config.Deployment,
 		ipAddress,
 		d.log,
		d.appinfo,
 	)
}

func (d *InfluxDbFirehoseNozzle) consumeFirehose(authToken string) {
	d.consumer = consumer.New(
		d.config.TrafficControllerURL,
		&tls.Config{InsecureSkipVerify: d.config.SsLSkipVerify},
		nil)
	d.consumer.SetIdleTimeout(time.Duration(d.config.IdleTimeoutSeconds) * time.Second)
	d.messages, d.errs = d.consumer.Firehose(d.config.FirehoseSubscriptionID, authToken)
}

func (d *InfluxDbFirehoseNozzle) postToInfluxDb() error {
	ticker := time.NewTicker(time.Duration(d.config.FlushDurationSeconds) * time.Second)
	for {
		select {
		case <-ticker.C:
			d.postMetrics()
		case envelope := <-d.messages:
			if !d.keepMessage(envelope) {
				continue
			}

			d.handleMessage(envelope)
			d.client.AddMetric(envelope)
		case err := <-d.errs:
			d.handleError(err)
			return err
		}
	}
}

func (d *InfluxDbFirehoseNozzle) postMetrics() {
	err := d.client.PostMetrics()
	if err != nil {
		d.log.Fatalf("FATAL ERROR: %s\n\n", err)
	}
}

func (d *InfluxDbFirehoseNozzle) handleError(err error) {
	if retryErr, ok := err.(noaaerrors.RetryError); ok {
		err = retryErr.Err
	}

	switch closeErr := err.(type) {
	case *websocket.CloseError:
		switch closeErr.Code {
		case websocket.CloseNormalClosure:
		// no op
		case websocket.ClosePolicyViolation:
			d.log.Errorf("Error while reading from the firehose: %v", err)
			d.log.Errorf("Disconnected because nozzle couldn't keep up. Please try scaling up the nozzle.")
			d.client.AlertSlowConsumerError()
		default:
			d.log.Errorf("Error while reading from the firehose: %v", err)
		}
	default:
		d.log.Infof("Error while reading from the firehose: %v", err)

	}

	d.log.Infof("Closing connection with traffic controller due to %v", err)
	d.consumer.Close()
	d.postMetrics()
}

func (d *InfluxDbFirehoseNozzle) keepMessage(envelope *events.Envelope) bool {
	var event string 

	switch envelope.GetEventType() {
	case events.Envelope_ContainerMetric:
		event = "ContainerMetric"
	case events.Envelope_CounterEvent:
		event = "CounterEvent"
	case events.Envelope_HttpStartStop:
		event = "HttpStartStop"
	case events.Envelope_ValueMetric:
		event = "ValueMetric"
	default:
		event = "unsupported"
		return false
	}
				
	return (d.config.DeploymentFilter == "" || d.config.DeploymentFilter == envelope.GetDeployment()) && (d.config.EventFilter == "" || strings.Contains(d.config.EventFilter, event))
}

func (d *InfluxDbFirehoseNozzle) handleMessage(envelope *events.Envelope) {
	if envelope.GetEventType() == events.Envelope_CounterEvent && envelope.CounterEvent.GetName() == "TruncatingBuffer.DroppedMessages" && envelope.GetOrigin() == "doppler" {
		d.log.Infof("We've intercepted an upstream message which indicates that the nozzle or the TrafficController is not keeping up. Please try scaling up the nozzle.")
		d.client.AlertSlowConsumerError()
	}
}
