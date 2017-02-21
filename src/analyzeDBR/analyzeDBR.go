package main

import (
	"fmt"
  "log"
  "github.com/BurntSushi/toml"
	"flag"
	"io/ioutil"
	"os"
	"errors"
	"regexp"
	"encoding/json"
	"net/http"
	"bytes"
	"strings"
)

type General struct {
  Namespace string
}

type RI struct {
  Enabled int `toml:"enableRIanalysis"`
  CwName  string
  CwDimension string
  CwUnit string
}

type Athena struct {
  DbSQL string `toml:"create_database"`
  TableSQL string `toml:"create_table"`
	TableBlendedSQL string `toml:"create_table_blended"`
	Test string
}

type Metric struct {
  Desc string
  Type string
  SQL string
  CwName string
  CwDimension string
  CwType string
}

type Config struct {
  General General
  RI RI
  Athena Athena
  Metrics []Metric
}

type AthenaRequest struct {
	AthenaUrl string `json:"athenaUrl"`
	S3StagingDir string `json:"s3StagingDir"`
	AwsSecretKey string `json:"awsSecretKey"`
	AwsAccessKey string `json:"awsAccessKey"`
	Query string `json:"query"`
}

type AthenaResponse struct {
	Columns []map[string]string
	Rows []map[string]string
}

var defaultConfigPath = "./analyzeDBR.config"


func getParams(configFile *string, account *string, region *string, key *string, secret *string, date *string, bucket *string, blended *int) error {

	// Define input command line config parameter and parse it
	flag.StringVar(configFile, "config", defaultConfigPath, "Input config file for analyzeDBR")
	flag.StringVar(key, "key", "", "Athena IAM access key")
	flag.StringVar(secret, "secret", "", "Athena IAM secret key")
	flag.StringVar(region, "region", "", "Athena Region")
	flag.StringVar(account, "account", "", "AWS Account #")
	flag.StringVar(date, "date", "", "Current month in YYYY-MM format")
	flag.StringVar(bucket, "bucket", "", "AWS Bucket where DBR files sit")
	flag.IntVar(blended, "blended", 0, "Set to 1 if DBR file contains blended costs")

	flag.Parse()

	// check input against defined regex's
	r_empty 	:= regexp.MustCompile(`^$`)
	r_region 	:= regexp.MustCompile(`^\w+-\w+-\d$`)
	r_account	:= regexp.MustCompile(`^\d+$`)
	r_date	  := regexp.MustCompile(`^\d{6}$`)

	if r_empty.MatchString(*key) {
		return errors.New("Must provide Athena access key")
	}
	if r_empty.MatchString(*secret) {
		return errors.New("Must provide Athena secret key")
	}
	if r_empty.MatchString(*bucket) {
		return errors.New("Must provide valid AWS DBR bucket")
	}
	if ! r_region.MatchString(*region) {
		return errors.New("Must provide valid AWS region")
	}
	if ! r_account.MatchString(*account) {
		return errors.New("Must provide valid AWS account number")
	}
	if ! r_date.MatchString(*date) {
		return errors.New("Must provide valid date (YYYY-MM)")
	}

	return nil
}


func getConfig(conf *Config, configFile string) error {

    // check for existance of file
    if _, err := os.Stat(configFile); err != nil {
      return errors.New("Config File " + configFile + " does not exist")
    }

    // read file
    b, err := ioutil.ReadFile(configFile)
		if err != nil {
      return err
    }

    // parse TOML config file into struct
    if _, err := toml.Decode(string(b), &conf); err != nil {
      return err
    }

    return nil
}

func substituteParams(sql string, params map[string]string) string {

	for sub, value := range params {
		sql = strings.Replace(sql, sub, value, -1)
	}

	return sql
}

func sendQuery (key string, secret string, region string, account string, sql string) (AthenaResponse, error) {

	// construct json
	req := AthenaRequest {
					AwsAccessKey: key,
					AwsSecretKey: secret,
					AthenaUrl: "jdbc:awsathena://athena." + region + ".amazonaws.com:443",
					S3StagingDir: "s3://aws-athena-query-results-" + account + "-" + region + "/",
					Query: sql }

	// encode into JSON
	b := new(bytes.Buffer)
	err := json.NewEncoder(b).Encode(req)
	if err != nil {
		return AthenaResponse{}, err
	}

	// send request through proxy
	resp, err := http.Post("http://127.0.0.1:10000/query", "application/json", b)
	if err != nil {
		return AthenaResponse{}, err
	}

	// check status code
	if resp.StatusCode != 200 {
		respBytes, _ := ioutil.ReadAll(resp.Body)
		return AthenaResponse{}, errors.New(string(respBytes))
	}

	// decode json into response struct
	var results AthenaResponse
	err = json.NewDecoder(resp.Body).Decode(&results)
	if err != nil {
		respBytes, _ := ioutil.ReadAll(resp.Body)
		return AthenaResponse{}, errors.New(string(respBytes))
	}

	return results, nil
}

func main() {

	var configFile, region, key, secret, account, bucket, date string
	var blendedDBR int
	if err := getParams(&configFile, &account, &region, &key, &secret, &date, &bucket, &blendedDBR); err != nil {
		log.Fatal(err)
	}

  var conf Config
  if err := getConfig(&conf, configFile); err != nil {
    log.Fatal(err)
  }

	// make sure Athena DB exists - dont care about results
	if _, err := sendQuery(key, secret, region, account, conf.Athena.DbSQL); err != nil {
		log.Fatal(err)
	}

	// make sure current Athena table exists - dont care about results
	// Depending on the Type of DBR (blended or not blended - the table we create is slightly different)
	if blendedDBR == 0 {
		if _, err := sendQuery(key, secret, region, account, substituteParams(conf.Athena.TableSQL, map[string]string{"**BUCKET**": bucket, "**DATE**": date, "**ACCOUNT**": account})); err != nil {
			log.Fatal(err)
		}
		fmt.Println("yay!")
	} else if (blendedDBR == 1) {
		if _, err := sendQuery(key, secret, region, account, substituteParams(conf.Athena.TableBlendedSQL, map[string]string{"**BUCKET**": bucket, "**DATE**": date, "**ACCOUNT**": account})); err != nil {
			log.Fatal(err)
		}
		fmt.Println("yayblended!")
	}

}
