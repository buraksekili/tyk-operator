package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
)

var cluster = flag.String("cluster", "kind", "cluster name")
var mode = flag.String("mode", "PRO", "TYK Mode (CE or PRO)")

func createKindCluster() {
	var buf bytes.Buffer

	cmd := exec.Command("kind", "get", "clusters")
	cmd.Stdout = &buf
	if err := cmd.Run(); err != nil {
		log.Fatalf("cannot run %s, err: %v", cmd.String(), err)
	}

	if strings.Contains(buf.String(), *cluster) {
		fmt.Printf("you already have a cluster called %v, skipping cluster creation\n", *cluster)
		return
	}

	cmd = exec.Command("make", "create-kind-cluster")

	mw := io.MultiWriter(os.Stdout, &buf)
	cmd.Stdout = mw
	cmd.Stderr = mw

	if err := cmd.Run(); err != nil {
		log.Fatalf("cannot run %s, err: %v", cmd.String(), err)
	}
}

// TODO: check if storage stack already exists
func createStorageStack() {
	namespace := "tykce-control-plane"
	if *mode == "PRO" {
		namespace = "tykpro-control-plane"

		fmt.Print("Installing Mongo ")
		cmd := exec.Command("helm", "install", "mongo", "tyk-helm/simple-mongodb", "-n", namespace)
		if err := cmd.Run(); err != nil {
			log.Fatalf("cannot run %s, err: %v", cmd.String(), err)
		}
		fmt.Println("DONE")
	}

	fmt.Print("Installing Redis ")
	cmd := exec.Command("helm", "install", "redis", "tyk-helm/simple-redis", "-n", namespace)
	if err := cmd.Run(); err != nil {
		log.Fatalf("cannot run %s, err: %v", cmd.String(), err)
	}
	fmt.Println("DONE")
}

func createNamespace() {
	namespace := "tykpro-control-plane"
	if *mode == "CE" {
		namespace = "tykce-control-plane"
	}

	var buf bytes.Buffer
	cmd := exec.Command("kubectl", "get", "namespaces")
	cmd.Stdout = &buf
	if err := cmd.Run(); err != nil {
		log.Fatalf("cannot run %s, err: %v", cmd.String(), err)
	}

	if strings.Contains(buf.String(), namespace) {
		fmt.Printf("you already have a namespace called %v, skipping namespace creation\n", namespace)
		return
	}

	cmd = exec.Command("kubectl", "create", "namespace", namespace)
	if err := cmd.Run(); err != nil {
		log.Fatalf("cannot run %s, err: %v", cmd.String(), err)
	}
}

// TODO: check if Tyk already exists
func installTyk() {
	tykVersion := "tyk-pro"
	tykHelmAddr := "tyk-helm/tyk-pro"
	namespace := "tykpro-control-plane"
	valuesFile := "hack/development/values-pro.yaml"

	if *mode == "CE" {
		tykVersion = "tyk-ce"
		tykHelmAddr = "tyk-helm/tyk-headless"
		namespace = "tykce-control-plane"
		valuesFile = "ci/helm/tyk-headless/values.yaml"
	}

	fmt.Printf("Installing Tyk-%v ", *mode)
	cmd := exec.Command("helm", "install", tykVersion, tykHelmAddr, "-f", valuesFile, "-n", namespace)
	if err := cmd.Run(); err != nil {
		log.Fatalf("cannot run %s, err: %v", cmd.String(), err)
	}

	fmt.Println("DONE")
}

func extractTykConfig() {
	namespace := "tykce-control-plane"
	if *mode == "PRO" {
		namespace = "tykpro-control-plane"
	}

	var buf bytes.Buffer
	cmd := exec.Command("kubectl", "get", "secret", "tyk-operator-conf", "-n", namespace, "-o", "json")
	cmd.Stdout = &buf
	if err := cmd.Run(); err != nil {
		log.Fatalf("cannot run %s, err: %v", cmd.String(), err)
	}

	o := struct {
		Data map[string]string `json:"data"`
	}{}

	if err := json.Unmarshal(buf.Bytes(), &o); err != nil {
		log.Fatalf("cannot unmarshal secret into struct, err: %v", err)
	}

	for k, v := range o.Data {
		x, _ := base64.StdEncoding.DecodeString(v)
		o.Data[k] = string(x)
	}
	auth := o.Data["TYK_AUTH"]
	org := o.Data["TYK_ORG"]
	tykMode := o.Data["TYK_MODE"]
	tykURL := o.Data["TYK_URL"]

	fmt.Printf("AUTH: %v\nORG: %v\nMODE: %v\nURL: %v\n", auth, org, tykMode, tykURL)
}

// TODO:
// 	1. Add more accurate error handling. At the moment, the script just prints status code and which code is not running.
// 		We require more detailed error description like we see in the terminal.
//	2. Decrease code duplication.
// 	3. Read YAML file for CE configuration.
func main() {
	flag.Parse()
	*mode = strings.ToUpper(*mode)

	// 1. Create KinD cluster.
	createKindCluster()

	// 2. Install CRDs
	fmt.Print("Installing CRDs into the cluster ")
	cmd := exec.Command("make", "install")
	if err := cmd.Run(); err != nil {
		log.Fatalf("cannot run %s, err: %v", cmd.String(), err)
	}
	fmt.Println("DONE!")

	// 3. Create a namespace for Tyk installation.
	fmt.Print("Creating a namespace for Tyk installation ")
	createNamespace()
	fmt.Println("DONE!")

	// 4. Add and update Helm charts of the Tyk
	cmd = exec.Command("helm", "repo", "add", "tyk-helm", "https://helm.tyk.io/public/helm/charts/")
	if err := cmd.Run(); err != nil {
		log.Fatalf("cannot run %s, err: %v", cmd.String(), err)
	}

	cmd = exec.Command("helm", "repo", "update")
	if err := cmd.Run(); err != nil {
		log.Fatalf("cannot run %s, err: %v", cmd.String(), err)
	}

	// 5. Installing Redis and Mongo based on Tyk installation type. If CE is installed, no need to install Mongo. Otherwise,
	// install Redis and Mongo.
	createStorageStack()

	// 6. Install Tyk
	installTyk()

	//extractTykConfig()
}
