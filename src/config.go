package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/confluentinc/confluent-kafka-go/kafka"

	yaml "gopkg.in/yaml.v2"
)

const initConfig = `schema.version: "1.0"
# tls.cert.file: my-domain.crt
# tls.key.file: my-domain.key
kafka.to.websocket:
  - kafka.consumer.config:
      # https://github.com/edenhill/librdkafka/blob/master/CONFIGURATION.md#global-configuration-properties
      metadata.broker.list: localhost:9092
      enable.auto.commit: false
      group.id: my-kafka-group
    kafka.default.topic.config:
      # https://github.com/edenhill/librdkafka/blob/master/CONFIGURATION.md#topic-configuration-properties
      auto.offset.reset: latest
    kafka.topics:
      - my.kafka.topic
    address: :9999
    # message.details: false
    # message.type: json
    # endpoint.prefix: ""
    # endpoint.websocket: ws
    # endpoint.test: test
`

// ConfigKWS Kafka to websocket YAML
type ConfigKWS struct {
	KafkaConsumerConfig     kafka.ConfigMap `yaml:"kafka.consumer.config"`
	KafkaDefaultTopicConfig kafka.ConfigMap `yaml:"kafka.default.topic.config"`
	KafkaTopics             []string        `yaml:"kafka.topics"`
	Address                 string          `yaml:"address"`
	EndpointPrefix          string          `yaml:"endpoint.prefix"`
	EndpointTest            string          `yaml:"endpoint.test"`
	EndpointWS              string          `yaml:"endpoint.websocket"`
	MessageDetails          bool            `yaml:"message.details"`
	MessageType             string          `yaml:"message.type"`
	Compression             bool            `yaml:"compression"`
}

// Config YAML config file
type Config struct {
	SchemaVersion string      `yaml:"schema.version"`
	TLSCertFile   string      `yaml:"tls.cert.file"`
	TLSKeyFile    string      `yaml:"tls.key.file"`
	ConfigKWSs    []ConfigKWS `yaml:"kafka.to.websocket"`
}

// ReadKWS read config file and returns collection of KWS
func ReadKWS(filename string) []*KWS {
	fileContent, err := ioutil.ReadFile(filename)
	if err != nil {
		absPath, _ := filepath.Abs(filename)
		log.Fatalf("Error while reading %v file: \n%v ", absPath, err)
	}
	log.Printf("%s\n%s", filename, string(fileContent))
	var config Config
	err = yaml.Unmarshal(fileContent, &config)
	if err != nil {
		log.Fatalf("Unmarshal: %v", err)
	}
	certFile := ""
	keyFile := ""
	if config.TLSCertFile != "" && config.TLSKeyFile == "" || config.TLSCertFile == "" && config.TLSKeyFile != "" {
		panic(fmt.Sprintf("Both certificate and key file must be defined"))
	} else if config.TLSCertFile != "" {
		if _, err := os.Stat(config.TLSCertFile); err == nil {
			if _, err := os.Stat(config.TLSKeyFile); err == nil {
				keyFile = config.TLSKeyFile
				certFile = config.TLSCertFile
			} else {
				panic(fmt.Sprintf("key file %s does not exist", config.TLSKeyFile))
			}
		} else {
			panic(fmt.Sprintf("certificate file %s does not exist", config.TLSKeyFile))
		}
	}
	kwsMap := make(map[string]*KWS)
	for _, kwsc := range config.ConfigKWSs {
		var kws *KWS
		var exists bool
		if kws, exists = kwsMap[kwsc.Address]; !exists {
			kws = &KWS{
				Address:     kwsc.Address,
				TLSCertFile: certFile,
				TLSKeyFile:  keyFile,
				WebSockets:  make(map[string]*KWSKafka),
				TestUIs:     make(map[string]*string),
			}
			kwsMap[kwsc.Address] = kws
		}
		if kwsc.MessageType == "" {
			kwsc.MessageType = "json"
		}
		testPath := kwsc.EndpointTest
		wsPath := kwsc.EndpointWS
		if testPath == "" && wsPath == "" {
			testPath = "test"
		}
		if kwsc.EndpointPrefix != "" {
			testPath = kwsc.EndpointPrefix + "/" + testPath
			wsPath = kwsc.EndpointPrefix + "/" + wsPath
		}
		testPath = "/" + strings.TrimRight(testPath, "/")
		wsPath = "/" + strings.TrimRight(wsPath, "/")

		if testPath == wsPath {
			panic(fmt.Sprintf("test path and websocket path can't be same [%s]", kwsc.EndpointTest))
		}
		if kwsc.KafkaConsumerConfig["metadata.broker.list"] == "" {
			panic(fmt.Sprintf("metadata.broker.list must be defined, address [%s]", kwsc.Address))
		}
		if kwsc.KafkaConsumerConfig["group.id"] == "" {
			panic(fmt.Sprintf("group.id must be defined, address [%s]", kwsc.Address))
		}
		if _, exists := kws.TestUIs[testPath]; exists {
			panic(fmt.Sprintf("test path [%s] already defined", testPath))
		}
		if _, exists := kws.WebSockets[testPath]; exists {
			panic(fmt.Sprintf("test path [%s] already defined as websocket path", testPath))
		}
		if _, exists := kws.WebSockets[wsPath]; exists {
			panic(fmt.Sprintf("websocket path [%s] already defined", wsPath))
		}
		if _, exists := kws.TestUIs[wsPath]; exists {
			panic(fmt.Sprintf("websocket path [%s] already defined as test path", wsPath))
		}
		if kwsc.MessageType != "json" &&
			kwsc.MessageType != "text" &&
			kwsc.MessageType != "binary" {
			panic(fmt.Sprintf("invalid message.type [%s]", kwsc.MessageType))
		}
		kws.TestUIs[testPath] = &wsPath
		kws.WebSockets[wsPath] = &KWSKafka{
			KafkaConsumerConfig:     kwsc.KafkaConsumerConfig,
			KafkaDefaultTopicConfig: kwsc.KafkaDefaultTopicConfig,
			KafkaTopics:             kwsc.KafkaTopics,
			MessageDetails:          kwsc.MessageDetails,
			MessageType:             kwsc.MessageType,
			Compression:             kwsc.Compression,
		}
	}
	kwss := make([]*KWS, len(kwsMap))
	i := 0
	for _, kws := range kwsMap {
		kwss[i] = kws
		i++
	}
	return kwss
}
