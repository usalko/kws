package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
)

const productVersion = "1.0.0"
const releaseTag = "lola simon"

func main() {
	configFile := flag.String("config", "config.yaml", "Config file location")
	initiate := flag.Bool("init", false, "Create initial config file")
	version := flag.Bool("v", false, "Print product version")
	flag.Parse()

	if *version {
		fmt.Printf("Kws %s (%s)\n", productVersion, releaseTag)
	} else if *initiate {
		if _, err := os.Stat(*configFile); os.IsNotExist(err) {
			if err := ioutil.WriteFile(*configFile, []byte(initConfig), 0644); err == nil {
				fmt.Printf("Config file %s successkfully created.\n", *configFile)
			} else {
				fmt.Printf("Can't create config file %s :\n%v", *configFile, err)
			}
		} else {
			fmt.Printf("Config file %s already exists.\n", *configFile)
		}
	} else {
		list := ReadKWS(*configFile)
		for i := range list {
			go func(kws *KWS) {
				err := kws.Start()
				if err != nil {
					log.Fatalln(err)
				}
			}(list[i])
		}

		var chExit = make(chan os.Signal, 1)
		signal.Notify(chExit, os.Interrupt)
		<-chExit
	}
}
