package main

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/confluentinc/confluent-kafka-go/kafka"
	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsflate"
	"github.com/gobwas/ws/wsutil"
)

// KWS Kafka to websocket config
type KWS struct {
	Address     string
	TLSCertFile string
	TLSKeyFile  string
	WebSockets  map[string]*KWSKafka
	TestUIs     map[string]*string
}

// KWSKafka Kafka config
type KWSKafka struct {
	KafkaConsumerConfig     kafka.ConfigMap
	KafkaDefaultTopicConfig kafka.ConfigMap
	KafkaTopics             []string
	MessageDetails          bool
	MessageType             string
	Compression             bool
}

type templateInfo struct {
	TestPath, WSURL string
}

var localStatic = false
var testTemplate *template.Template
var rexStatic = regexp.MustCompile(`(.*)(/static/.+(\.[a-z0-9]+))$`)
var mimeTypes = map[string]string{
	".js":   "application/javascript",
	".htm":  "text/html; charset=utf-8",
	".html": "text/html; charset=utf-8",
	".css":  "text/css; charset=utf-8",
	".json": "application/json",
	".xml":  "text/xml; charset=utf-8",
	".jpg":  "image/jpeg",
	".png":  "image/png",
	".svg":  "image/svg+xml",
	".gif":  "image/gif",
	".pdf":  "application/pdf",
}

func parseQueryString(query url.Values, key string, val string) string {
	if val == "" {
		if vals, exists := query[key]; exists && len(vals) > 0 {
			return vals[0]
		}
	}
	return val
}

// Start start websocket and start consuming from Kafka topic(s)
func (kws *KWS) Start() error {
	if kws.TLSCertFile != "" {
		return http.ListenAndServeTLS(kws.Address, kws.TLSCertFile, kws.TLSKeyFile, kws)
	}
	return http.ListenAndServe(kws.Address, kws)
}

func copyConfigMap(m kafka.ConfigMap) kafka.ConfigMap {
	nm := make(kafka.ConfigMap)
	for k, v := range m {
		nm[k] = v
	}
	return nm
}

func (kws *KWS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	submatch := rexStatic.FindStringSubmatch(r.URL.Path)
	if len(submatch) > 0 {
		// serve static files
		testUIPath := submatch[1]
		if testUIPath == "" {
			testUIPath = "/"
		}
		if _, exists := kws.TestUIs[testUIPath]; exists {
			if payload, err := FSByte(localStatic, submatch[2]); err == nil {
				var mime = "application/octet-stream"
				if m, ok := mimeTypes[submatch[3]]; ok {
					mime = m
				}
				w.Header().Add("Content-Type", mime)
				w.Header().Add("Content-Length", fmt.Sprintf("%d", len(payload)))
				w.Write(payload)
				return
			}
		}
	} else if wsPath, exists := kws.TestUIs[r.URL.Path]; exists {
		html, err := FSString(localStatic, "/static/test.html")
		if err == nil {
			wsURL := "ws://" + r.Host + *wsPath
			if kws.TLSCertFile != "" {
				wsURL = "wss://" + r.Host + *wsPath
			}
			if testTemplate == nil || localStatic {
				testTemplate = template.Must(template.New("").Parse(html))
			}
			testTemplate.Execute(w, templateInfo{strings.TrimRight(r.URL.Path, "/"), wsURL})
		}
		return
	} else if kcfg, exists := kws.WebSockets[r.URL.Path]; exists {

		upgrader := ws.HTTPUpgrader{}
		if kcfg.Compression {
			e := wsflate.Extension{
				// We are using default parameters here since we use
				// wsflate.{Compress,Decompress}Frame helpers below in the code.
				// This assumes that we use standard compress/flate package as flate
				// implementation.
				Parameters: wsflate.DefaultParameters,
			}
			upgrader = ws.HTTPUpgrader{
				Negotiate: e.Negotiate,
			}
		}
		wscon, _, _, err := upgrader.Upgrade(r, w)

		if err != nil {
			log.Printf("Websocket http upgrade failed: %v\n", err)
			return
		}

		// Read kafka params from query string
		query := r.URL.Query()
		config := copyConfigMap(kcfg.KafkaConsumerConfig)
		// config["debug"] = "protocol"
		// config["broker.version.fallback"] = "0.8.0"
		config["session.timeout.ms"] = 6000
		config["go.events.channel.enable"] = true
		config["go.application.rebalance.enable"] = true
		if config["group.id"] == nil {
			config["group.id"] = parseQueryString(query, "group.id", "")
		}
		defTopic := copyConfigMap(kcfg.KafkaDefaultTopicConfig)
		if defTopic["auto.offset.reset"] == nil {
			defTopic["auto.offset.reset"] = parseQueryString(query, "auto.offset.reset", "")
		}
		config["default.topic.config"] = defTopic
		topics := kcfg.KafkaTopics
		if len(topics) == 0 {
			if t := parseQueryString(query, "topics", ""); t != "" {
				topics = strings.Split(t, ",")
			} else {
				log.Printf("No topic(s)")
				return
			}
		}

		// Instantiate consumer
		consumer, err := kafka.NewConsumer(&config)
		if err != nil {
			fmt.Printf("Can't create consumer: %v\n", err)
			return
		}
		defer consumer.Close()

		// Make sure all topics actually exist
		meta, err := consumer.GetMetadata(nil, true, 5000)
		if err != nil {
			fmt.Printf("Can't get metadata: %v\n", err)
			return
		}
		for _, topic := range topics {
			if _, exists := meta.Topics[topic]; !exists {
				log.Printf("Topic [%s] doesn't exist", topic)
				return
			}
		}

		// Make sure to read client message and react on close/error
		chClose := make(chan bool)

		go func() {
			defer wscon.Close()

			for {
				_, _, err := wsutil.ReadClientData(wscon)
				if err != nil {
					// handle error
					chClose <- true
					if !strings.HasPrefix(err.Error(), "websocket: close") {
						log.Printf("WebSocket read error: %v\n", err)
					}
					return
				}
			}
		}()

		// Subscribe consumer to the topics
		err = consumer.SubscribeTopics(topics, nil)
		if err != nil {
			fmt.Printf("Can't subscribe costumer: %v\n", err)
			return
		}
		defer consumer.Unsubscribe()

		log.Printf("Websocket opened %s\n", r.Host)
		// websocketMessageType := ws.TextMessage
		// if kcfg.MessageType == "binary" {
		// 	websocketMessageType = ws.BinaryMessage
		// }
		running := true
		// Keep reading and sending messages
		for running {
			select {
			// Exit if websocket read fails
			case <-chClose:
				running = false
			case ev := <-consumer.Events():
				switch e := ev.(type) {
				case kafka.AssignedPartitions:
					consumer.Assign(e.Partitions)
				case kafka.RevokedPartitions:
					consumer.Unassign()
				case *kafka.Message:
					if kcfg.MessageDetails {
						val, err2 := JSONBytefy(e, kcfg.MessageType)
						if err2 == nil {
							err = wsutil.WriteServerMessage(wscon, ws.OpBinary, val)
						} else {
							err = wsutil.WriteServerMessage(wscon, ws.OpBinary, e.Value)
						}

					} else {
						err = wsutil.WriteServerMessage(wscon, ws.OpBinary, e.Value)
					}
					if err != nil {
						// handle error
						chClose <- true
						log.Printf("WebSocket write error: %v (%v)\n", err, e)
						running = false
					}
				case kafka.PartitionEOF:
					// log.Printf("%% Reached %v\n", e)
				case kafka.Error:
					log.Printf("%% Error: %v\n", e)
					err = wscon.Close()
					if err != nil {
						log.Printf("Error while closing WebSocket: %v\n", e)
					}
					running = false
				}
			}
		}
		log.Printf("Websocket closed %s\n", r.Host)
		return
	}
	w.WriteHeader(404)
}
