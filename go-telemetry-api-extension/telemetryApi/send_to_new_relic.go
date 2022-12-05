package telemetryApi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path"
	"reflect"
	"strings"
	"time"
)

const (
	LogEndpointEU string = "https://log-api.eu.newrelic.com/log/v1"
	LogEndpointUS string = "https://log-api.newrelic.com/log/v1"

	MetricsEndpointEU string = "https://metric-api.eu.newrelic.com/metric/v1"
	MetricsEndpointUS string = "https://metric-api.newrelic.com/metric/v1"

	EventsEndpointEU string = "https://insights-collector.eu01.nr-data.net"
	EventsEndpointUS string = "https://insights-collector.newrelic.com"

	TracesEndpointEU string = "https://trace-api.eu.newrelic.com/trace/v1"
	TracesEndpointUS string = "https://trace-api.newrelic.com/trace/v1"
)

func getEndpointURL(licenseKey string, typ string, EndpointOverride string) string {
	if EndpointOverride != "" {
		return EndpointOverride
	}
	switch typ {
	case "logging":
		if strings.HasPrefix(licenseKey, "eu") {
			return LogEndpointEU
		} else {
			return LogEndpointUS
		}
	case "metrics":
		if strings.HasPrefix(licenseKey, "eu") {
			return MetricsEndpointEU
		} else {
			return MetricsEndpointUS
		}
	case "events":
		if strings.HasPrefix(licenseKey, "eu") {
			return EventsEndpointEU
		} else {
			return EventsEndpointUS
		}
	case "traces":
		if strings.HasPrefix(licenseKey, "eu") {
			return TracesEndpointEU
		} else {
			return TracesEndpointUS
		}
	}
	return ""
}

func sendBatch(ctx context.Context, d *Dispatcher, uri string, bodyBytes []byte) error {
	req, err := http.NewRequestWithContext(ctx, "POST", uri, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return err
	}
	// the headers might be different for different endpoints
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Api-Key", d.licenseKey)
	_, err = d.httpClient.Do(req)

	return err
}

func sendDataToNR(ctx context.Context, logEntries []interface{}, d *Dispatcher) error {

	// will be replaced later
	var lambda_name = "---"
	// should be as below
	//        var lambda_name = d.functionName
	var agent_name = path.Base(os.Args[0])

	// NB "." is not allowed in NR eventType
	var replacer = strings.NewReplacer(".", "_")

	data := make(map[string][]map[string]interface{})
	data["events"] = []map[string]interface{}{}
	data["traces"] = []map[string]interface{}{}
	data["logging"] = []map[string]interface{}{}
	data["metrics"] = []map[string]interface{}{}

	var bb map[string]any
	//	var event map[string]any

	for _, event := range logEntries {
		//json.Unmarshal([]byte(ev), &event)
		msInt, err := time.Parse(time.RFC3339, event.(LambdaTelemetryEvent).Time)
		if err != nil {
			return err
		}
		// events
		json.Unmarshal([]byte(`{
                        "timestamp": msInt.UnixMilli()
                        "eventType": "Lambda_Ext_"+ replacer.Replace(event["type"])
                }`), &bb)
		data["events"] = append(data["events"], bb)

		data["events"] = append(data["events"], map[string]interface{}{
			"timestamp": msInt.UnixMilli(),
			"eventType": "Lambda_Ext_" + replacer.Replace(event.(LambdaTelemetryEvent).Type),
		})

		switch event.(LambdaTelemetryEvent).Type {
		case "platform.iniStart":

		case "platform.iniRuntimeDone":

		case "platform.iniReport":

		case "platform.start":

		case "platform.runtimeDone":

		case "platform.report":

		case "platform.extension":

		case "platform.telemetrySubscription":

		case "platform.logsDropped":

		}

		if event.(LambdaTelemetryEvent).Record != nil {
			data["logging"] = append(data["logging"], map[string]interface{}{
				"timestamp": msInt.UnixMilli(),
				"message":   event.(LambdaTelemetryEvent).Record,
				"attributes": map[string]map[string]string{
					"aws": {
						"event":  event.(LambdaTelemetryEvent).Type,
						"lambda": lambda_name,
					},
				},
			})
		}

		// metrics
		if event.(LambdaTelemetryEvent).Record != nil {
			if val, ok := event.(LambdaTelemetryEvent).Record["metrics"]; ok {
				mts := []map[string]interface{}{}
				for key := range val {
					mts := append(mts, map[string]interface{}(`{
						"name": "aws.telemetry.lambda_ext."+lambda_name+"."+key,
						"value": event["record"]["metrics"][key]
					}`))
				}
				rid := ""
				if val, ok := event["record"]["requestId"]; ok {
					rid = val
				}
				data["metrics"] = append(data["metrics"], map[string]interface{}(`{
					"common" : {
						"timestamp": msInt.UnixMilli(),
						"attributes": {
							"event": event["type"],
							"requestId": rid,
							"extension": agent_name
							}
					},
					"metrics": mts
				}`))
			}
		}
		// spans
		if reflect.ValueOf(event["record"]).Kind() == reflect.Map {
			if val, ok := event["record"]["spans"]; ok {
				spans := [...]string{}
				for span := range val {
					el := `{
						"trace.id": event["record"]["requestId"],
						"id": uuid.New().String(),
						"attributes": {
							"event": event["type"],
							"service.name": agent_name
							}
					}`
					start, err := time.Parse(time.RFC3339, event["time"])
					if err != nil {
						return err
					}
					for key := range span {
						if key == "durationMs" {
							el["attributes"]["duration.ms"] = span[key]
						} else if key == "start" {
							el["timestamp"] = start.UnixMilli()
						} else {
							el["attributes"][key] = span[key]
						}
					}
					data["traces"] = append(data["traces"], el)
				}
			}
		}
	}
	// data ready
	if len(data) > 0 {
		// send logs
		if len(data["logging"]) > 0 {
			//			bodyBytes, _ := json.Marshal(map[string]interface{}(`{
			bodyBytes := `{
				"common": {
					"attributes": {
						"aws": {
							"logType": "aws/lambda-ext",
							"function": lambda_name,
							"extension": agent_name
							}
						}
				},
				"logs": data["logging"]
			}`
			//			}`))
			err := sendBatch(ctx, d, getEndpointURL(d.licenseKey, "logging"), bodyBytes)
		}
		// send metrics
		if len(data["metrics"]) > 0 {
			for payload := range data["metrics"] {
				bodyBytes, _ := json.Marshal(payload)
				err := sendBatch(ctx, d, getEndpointURL(d.licenseKey, "metrics"), bodyBytes)
			}
		}
		// send events
		if len(data["events"]) > 0 {
			bodyBytes, _ := json.Marshal(data["events"])
			err := sendBatch(ctx, d, getEndpointURL(d.licenseKey, "events"), bodyBytes)
		}
		// send traces
		if len(data["traces"]) > 0 {
			bodyBytes, _ := json.Marshal(map[string]interface{}(`{
				"common": {
					"attributes": {
						"host": "aws.amazon.com",
						"service.name": lambda_name
					}
				},
				"spans": data["traces"]
			}`))
			err := sendBatch(ctx, d, getEndpointURL(d.licenseKey, "traces"), bodyBytes)
		}
	}

	return err // if one of the sents failed, it'd be nice to know which
}
