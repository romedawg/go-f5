package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/f5devcentral/go-bigip"
	"github.com/hashicorp/hcl"
	"github.com/pkg/errors"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path"
	"time"
)

var (
	username     = flag.String("username", "", "The f5 username needs to be set.")
	f5Host       = flag.String("f5Host", "", "This needs to set with the f5 hostname or address.")
	terraformDir = flag.String("terraformDir", "", "This needs to set with the terraform directory "+
		"with the VIP configuration files.")
)

// Config struct for the f5
type Config struct {
	host     string
	username string
	password string
	timeout  time.Duration
}

//  compare two maps - f5 vs Terraform
func compareVips(f5Map map[string][]string, terraformVips map[string][]string) (map[string][]string, error) {

	diffIrules := make(map[string][]string)

	for terraformVipName, terraformIrules := range terraformVips {
		f5Irules, ok := f5Map[terraformVipName]
		if !ok {
			return nil, errors.Errorf("f5 vip %s is not found in the terraform configs", terraformVipName)
		}
		if len(f5Irules) != len(terraformIrules) {
			diffIrules[terraformVipName] = f5Irules
			continue
		}
		for idx, tfIrule := range terraformIrules {
			if tfIrule != f5Irules[idx] {
				diffIrules[terraformVipName] = f5Irules
				break
			}
		}
	}
	return diffIrules, nil
}

func decodeJSON(dec *json.Decoder) (string, []string) {
	var (
		vipName   string
		vipirules []string
	)
	for {
		t, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatal(err)
		}
		propName, ok := t.(string)
		if !ok {
			continue
		}
		if propName == "name" {
			for dec.More() {
				t, err := dec.Token()
				if err != nil {
					log.Fatal("unexpected delimiter in the input", err)
				}
				value, ok := t.(string)
				if !ok {
					continue
				}
				vipName = value
				break
			}
		}
		if propName == "irules" {
			for dec.More() {
				t, err := dec.Token()
				if err != nil {
					log.Fatal("unexpected delimiter in the input", err)
				}
				rule, ok := t.(string)
				if !ok {
					continue
				}
				vipirules = append(vipirules, rule)
			}
		}
	}
	return vipName, vipirules
}

// f5Config initializes the config struct to be used for f5 connection
func f5Config(host string, username string, password string, timeout time.Duration) *Config {
	conf := &Config{}
	conf.host = host
	conf.username = username
	conf.timeout = time.Duration(timeout) * time.Second
	conf.password = password
	return conf
}

// f5VipMap Returns a map of vips/iRules that exist in the f5
func f5VipMap(vips *bigip.VirtualServers) map[string][]string {
	vipMap := make(map[string][]string)
	for _, vip := range vips.VirtualServers {
		vipMap[vip.Name] = vip.Rules
	}
	return vipMap
}

// parseTerraformFiles Iterates through the filenames passed in and returns two objects, Vip Name and iRules for the Vip
func parseTerraformFiles(dir string, filename string) (string, []string) {
	file, err := os.Open(path.Join(dir, filename))
	if err != nil {
		log.Fatal(err)
	}
	data, err := ioutil.ReadAll(file)
	if err != nil {
		log.Fatal(err)
	}

	// Using json conversion for simplicity. The hcl package api forces you to do AST walking.
	var v interface{}
	err = hcl.Unmarshal(data, &v)
	if err != nil {
		log.Fatalf("unable to parse HCL: %s", err)
	}

	jsonHCL, err := json.Marshal(v)
	if err != nil {
		log.Fatalf("unable to marshall json: %s", err)
	}

	dec := json.NewDecoder(bytes.NewBuffer(jsonHCL))

	return decodeJSON(dec)

}

// terraformFileNames Returns a slice of file names and the directory name containing terraform vip files
func terraformFileNames(directoryName string) ([]string, string, error) {

	var fileNames []string
	dir := directoryName
	dirEntries, err := ioutil.ReadDir(dir)
	if err != nil {
		log.Fatalf("coud not read directory: %+v", err)
	}

	for _, fi := range dirEntries {
		fileNames = append(fileNames, fi.Name())
	}
	return fileNames, dir, err
}

// terraformVipDetails Returns a map for Terraform Virtual address(vipname: irules)
func terraformVipDetails(dir string, fileNames []string) map[string][]string {
	terraformVips := make(map[string][]string)

	for _, name := range fileNames {
		vipName, vipirules := parseTerraformFiles(dir, name)
		terraformVips[vipName[8:]] = vipirules
	}
	return terraformVips
}

func main() {

	// Input Validation
	flag.Parse()
	if *username == "" {
		log.Fatal("flag -username is not set.")
	}
	if *f5Host == "" {
		log.Fatal("flag -f5Host is not set and the f5 hostname or address needs to be set.")
	}
	if *terraformDir == "" {
		log.Fatal("flag -terraformDir is not set, need the directory for terraform vip configs.")
	}

	// Pre F5 Connection
	timeout := 500
	password := os.Getenv("f5_password")
	if password == "" {
		log.Fatal("The f5 password is not valid, set f5_password as an environment variable")
	}
	conf := f5Config(*f5Host, *username, password, time.Duration(timeout))

	// F5: this setups the rest api connection and returns a object of Virtual Servers
	opts := &bigip.ConfigOptions{APICallTimeout: conf.timeout}
	f5Client := bigip.NewSession(conf.host, conf.username, conf.password, opts)
	f5Client.Transport.TLSClientConfig.InsecureSkipVerify = true
	ltmClient, err := f5Client.VirtualServers()
	if err != nil {
		errors.Wrap(err, "failed to create an f5Client")
	}

	// Terraform VIP configs - Returns a slice of terraform filenames and the directory passed in at runtime
	fileNames, dir, err := terraformFileNames(*terraformDir)
	if err != nil {
		log.Fatalf("could not get the file name from input: %+v", err)
	}

	// Load VIP from the F5
	f5Vips := f5VipMap(ltmClient)

	// Load VIP from Terraform configs
	terraformVips := terraformVipDetails(dir, fileNames)

	// This returns an iRule list for terraform VIPs that needs to be adjusted to match what is in the f5.
	// The iRules are returned as they are ordered in the f5.
	diffIrules, err := compareVips(f5Vips, terraformVips)
	if err != nil {
		log.Fatal(err)
	}

	enc := json.NewEncoder(os.Stdout)
	if len(diffIrules) != 0 {
		fmt.Print("These are the terraform vips that need to be fixed:\n")
		enc.Encode(diffIrules)
		os.Exit(1)
	}
	fmt.Print("The f5 UI and the terraform VIP configurations matchup, no iRules needs to be changed \n")
	os.Exit(0)
}
