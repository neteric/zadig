/*
Copyright 2021 The KodeRover Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
)

var BuildStamp = "No Build Stamp Provided"

func hello(w http.ResponseWriter, req *http.Request) {

	fmt.Fprintf(w, "hello, my name is Go~~\n")
}

func headers(w http.ResponseWriter, req *http.Request) {

	for name, headers := range req.Header {
		for _, h := range headers {
			fmt.Fprintf(w, "%v: %v\n", name, h)
		}
	}
}

func buildStamp(w http.ResponseWriter, req *http.Request) {

	fmt.Fprintf(w, "%s", BuildStamp)
}

// 获取本机hostname
func hostName() string {
	hostname, err := os.Hostname()
	if err == nil {
		return hostname
	}

	return err.Error()
}

// 获取本机IP地址
func hostIp() string {
	addrs, err := net.InterfaceAddrs()

	if err != nil {
		return err.Error()
	}

	ips := ""

	for _, address := range addrs {
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				ips = fmt.Sprintf("%s %s", ips, ipnet.IP.String())
			}
		}
	}

	return ips
}

func main() {

	log.Println("Hello, welcome to the microservice world.")

	http.HandleFunc("/", hello)
	http.HandleFunc("/api/buildstamp", buildStamp)
	http.HandleFunc("/headers", headers)
	http.HandleFunc("/health", func(w http.ResponseWriter, req *http.Request) {
		fmt.Fprintf(w, "ok")
	})
	http.HandleFunc("/serverinfo", func(w http.ResponseWriter, req *http.Request) {
		fmt.Fprintf(w, "hostname: %s\n", hostName())
		fmt.Fprintf(w, "hostip: %s\n", hostIp())
	})
	http.ListenAndServe(":20219", nil)
}
