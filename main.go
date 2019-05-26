package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"mysql/instance"
)

func main() {
	flag.Parse()
	instances,err := instance.GetMySQLInstances()
	if err != nil {
		log.Fatal(err)
	}
	if bs,err := json.MarshalIndent(instances,"","  ");err != nil {
		log.Fatal(err)
	}else {
		fmt.Println(string(bs))
	}
}