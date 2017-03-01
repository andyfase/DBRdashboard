package main

import (
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
	"time"
	"strconv"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudwatch"
  "github.com/aws/aws-sdk-go/service/ec2"
  "github.com/mohae/deepcopy"
)

type General struct {
  Namespace string
}

type RI struct {
  Enabled bool `toml:"enableRIanalysis"`
  TotalUtilization bool `toml:"enableRITotalUtilization"`
  PercentThreshold int `toml:"riPercentageThreshold"`
  TotalThreshold int `toml:"riTotalThreshold"`
  CwName  string
  CwNameTotal string
  CwDimension string
  CwType string
  Sql string
  Ignore map[string]int
}

type Athena struct {
  DbSQL string `toml:"create_database"`
  TableSQL string `toml:"create_table"`
	TableBlendedSQL string `toml:"create_table_blended"`
	Test string
}

type Metric struct {
	Enabled bool
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


func getParams(configFile *string, account *string, region *string, key *string, secret *string, date *string, bucket *string, blended *bool) error {

	// Define input command line config parameter and parse it
	flag.StringVar(configFile, "config", defaultConfigPath, "Input config file for analyzeDBR")
	flag.StringVar(key, "key", "", "Athena IAM access key")
	flag.StringVar(secret, "secret", "", "Athena IAM secret key")
	flag.StringVar(region, "region", "", "Athena Region")
	flag.StringVar(account, "account", "", "AWS Account #")
	flag.StringVar(date, "date", "", "Current month in YYYY-MM format")
	flag.StringVar(bucket, "bucket", "", "AWS Bucket where DBR files sit")
	flag.BoolVar(blended, "blended", false, "Set to 1 if DBR file contains blended costs")

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

func sendMultiDimensionMetric (svc *cloudwatch.CloudWatch, data AthenaResponse, cwNameSpace string, cwName string, cwType string, cwDimensionName string) error {

	input := cloudwatch.PutMetricDataInput{}
	input.Namespace = aws.String(cwNameSpace)

	i := 0
	for row := range data.Rows {
		// send Metric Data is got to 20 records, and clear MetricData Array
		if i >= 20 {
			_, err := svc.PutMetricData(&input)
			if err != nil {
					return err
			}
			input.MetricData = nil
			i = 0
		}

		t, _ := time.Parse("2006-01-02 15", data.Rows[row]["date"])
		v, _ := strconv.ParseFloat(data.Rows[row]["value"], 64)
		metric := cloudwatch.MetricDatum{
            MetricName: aws.String(cwName),
            Timestamp:  aws.Time(t),
            Unit:       aws.String(cwType),
						Value:			aws.Float64(v),
    }
		dimension := cloudwatch.Dimension{
									Name: aws.String(cwDimensionName),
									Value: aws.String(data.Rows[row]["dimension"]) }

		metric.Dimensions = append([]*cloudwatch.Dimension{}, &dimension)
		input.MetricData = append(input.MetricData, &metric)
		i++
	}

	_, err := svc.PutMetricData(&input)
  if err != nil {
      return err
  }

	return nil
}

func riUtilizationHour (svc *cloudwatch.CloudWatch, date string, used map[string]map[string]int, azRI map[string]map[string]int, regionRI map[string]int, conf Config, region string) error {

  // Perform Deep Copy of both RI maps.
  // We need a copy of the maps as we decrement the RI's available by the hourly usage and a map is a pointer
  // hence decrementing the original maps will affect the pass-by-reference data
  cpy := deepcopy.Copy(azRI)
  t_azRI, ok := cpy.(map[string]map[string]int)
  if ! ok {
    return errors.New("could not copy AZ RI map")
  }

  cpy = deepcopy.Copy(regionRI)
  t_regionRI, ok := cpy.(map[string]int)
  if ! ok {
    return errors.New("could not copy Regional RI map")
  }

  // Iterate through used hours decrementing any available RI's per hour's that were used
  // AZ specific RI's are first checked and then regional RI's
  for az := range used {
    for instance := range used[az] {
      // check if azRI for this region even exists
      _, ok := t_azRI[az][instance]
      if ok {
        // More RI's than we used
        if t_azRI[az][instance] >= used[az][instance] {
          t_azRI[az][instance] -= used[az][instance]
          used[az][instance] = 0
        } else {
          // Less RI's than we used
          used[az][instance] -= t_azRI[az][instance]
          t_azRI[az][instance] = 0
        }
      }

      // check if regionRI even exists and that instance used is in the right region
      _, ok = t_regionRI[instance]
      if ok && az[:len(az)-1] == region {
        // if we still have more used instances check against regional RI's
        if used[az][instance] > 0 && t_regionRI[instance] > 0 {
          if t_regionRI[instance] >= used[az][instance] {
            t_regionRI[instance] -= used[az][instance]
            used[az][instance] = 0
          } else {
            used[az][instance] -= t_regionRI[instance]
            t_regionRI[instance] = 0
          }
        }
      }
    }
  }

  // Now loop through the temp RI data to check if any RI's are still available
  // If they are and the % of un-use is above the configured threshold then colate for sending to cloudwatch
  // We sum up the total of regional and AZ specific RI's so that we get one instance based metric regardless of region or AZ RI
  i_unused := make(map[string]int)
  i_total  := make(map[string]int)
  var unused int
  var total int

  for az := range t_azRI {
    for instance := range t_azRI[az] {
      i_total[instance] = azRI[az][instance]
      i_unused[instance] = t_azRI[az][instance]

      total += azRI[az][instance]
      unused += t_azRI[az][instance]
    }
  }
  for instance := range t_regionRI {
    i_total[instance] += regionRI[instance]
    i_unused[instance] += t_regionRI[instance]
    total += regionRI[instance]
    unused += t_regionRI[instance]
  }

  // loop over per-instance utilization and build metrics to send
  metrics := AthenaResponse{}
  for instance := range i_unused {
    _, ok := conf.RI.Ignore[instance]
    if !ok { // instance not on ignore list
      percent := (float64(i_unused[instance]) / float64(i_total[instance])) * 100
      if int(percent) > conf.RI.PercentThreshold && i_total[instance] > conf.RI.TotalThreshold {
        fmt.Println("here")
        metrics.Rows = append(metrics.Rows, map[string]string{"dimension": instance, "date": date, "value": strconv.FormatInt(int64(percent), 10)})
      }
    }
  }

  // send per instance type under-utilization
  if len(metrics.Rows) > 0 {
    if err := sendMultiDimensionMetric(svc, metrics, conf.General.Namespace, conf.RI.CwName, conf.RI.CwType, conf.RI.CwDimension); err != nil {
      log.Fatal(err)
    }
  }

  // If confured send overall total utilization
  if conf.RI.TotalUtilization {
    percent := 100 - ((float64(unused) / float64(total)) * 100)

    total := AthenaResponse{}
    total.Rows = append(metrics.Rows, map[string]string{"dimension": "overall", "date": date, "value": strconv.FormatInt(int64(percent), 10)})

    if err := sendMultiDimensionMetric(svc, total, conf.General.Namespace, conf.RI.CwNameTotal, conf.RI.CwType, conf.RI.CwDimension); err != nil {
      log.Fatal(err)
    }
  }

  return nil
}

func riUtilization (sess *session.Session, conf Config, key string, secret string, region string, account string, date string) error {

  sess2, err := session.NewSessionWithOptions(session.Options{
  	 Config: aws.Config{Region: aws.String("us-east-1")},
  	 SharedConfigState: session.SharedConfigEnable,
      Profile: "hootsuite",
  })

  svc := ec2.New(sess2)

  params := &ec2.DescribeReservedInstancesInput{
    DryRun: aws.Bool(false),
    Filters: []*ec2.Filter{
       {
           Name: aws.String("state"),
           Values: []*string{
               aws.String("active"),
           },
       },
   },
 }

  resp, err := svc.DescribeReservedInstances(params)
  if err != nil {
    return err
  }

  az_ri := make(map[string]map[string]int)
  region_ri := make(map[string]int)

  // map in number of RI's available both AZ specific and regional
  for i := range resp.ReservedInstances {
    ri := resp.ReservedInstances[i]

    if *ri.Scope == "Availability Zone" {
      _, ok := az_ri[*ri.AvailabilityZone]
      if ! ok {
        az_ri[*ri.AvailabilityZone] = make(map[string]int)
      }
      az_ri[*ri.AvailabilityZone][*ri.InstanceType] += int(*ri.InstanceCount)
    } else if  *ri.Scope == "Region" {
      region_ri[*ri.InstanceType] += int(*ri.InstanceCount)
      }
  }

  // Fetch RI hours used
  data, err := sendQuery(key, secret, region, account, substituteParams(conf.RI.Sql, map[string]string{"**DATE**": date }))
  if err != nil {
    log.Fatal(err)
  }

  // loop through response data and generate map of hourly usage, per AZ.
  hours := make(map[string]map[string]map[string]int)
  for row := range data.Rows {
    _, ok := hours[data.Rows[row]["date"]]
    if ! ok {
      hours[data.Rows[row]["date"]] = make(map[string]map[string]int)
    }
    _, ok = hours[data.Rows[row]["date"]][data.Rows[row]["az"]]
    if ! ok {
      hours[data.Rows[row]["date"]][data.Rows[row]["az"]] = make(map[string]int)
    }

    v, _ := strconv.ParseInt(data.Rows[row]["hours"], 10, 64)
    hours[data.Rows[row]["date"]][data.Rows[row]["az"]][data.Rows[row]["instance"]] += int(v)
}

  // Create new cloudwatch client.
  svcCloudwatch := cloudwatch.New(sess)

  // Iterate through each hour and compare the number of instances used vs the number of RIs available
  // If RI leftover percentage is > 1% push to cloudwatch
  for hour := range hours {
    if err := riUtilizationHour(svcCloudwatch, hour, hours[hour], az_ri, region_ri, conf, region); err != nil {
      return err
    }
  }
  return nil
}


func main() {

	var configFile, region, key, secret, account, bucket, date, costColumn string
	var blendedDBR bool
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
	if blendedDBR {
		costColumn = "blendedcost"
		if _, err := sendQuery(key, secret, region, account, substituteParams(conf.Athena.TableBlendedSQL, map[string]string{"**BUCKET**": bucket, "**DATE**": date, "**ACCOUNT**": account})); err != nil {
			log.Fatal(err)
		}
	} else {
		costColumn = "cost"
		if _, err := sendQuery(key, secret, region, account, substituteParams(conf.Athena.TableSQL, map[string]string{"**BUCKET**": bucket, "**DATE**": date, "**ACCOUNT**": account})); err != nil {
			log.Fatal(err)
		}
	}

	/// initialize AWS GO client
	sess, err := session.NewSession(&aws.Config{Region: aws.String(region)})
	if err != nil {
		log.Fatal(err)
	}

	// Create new cloudwatch client.
	svc := cloudwatch.New(sess)

  // If RI analysis enabled - do it
  if conf.RI.Enabled {
    if err := riUtilization(sess, conf, key, secret, region, account, date); err != nil {
      log.Fatal(err)
    }
  }

  // testing
  log.Fatal("done")

	// iterate through metrics - perform query then send data to cloudwatch
	for metric := range conf.Metrics {
		if ! conf.Metrics[metric].Enabled {
			continue
		}
		results, err := sendQuery(key, secret, region, account, substituteParams(conf.Metrics[metric].SQL, map[string]string{"**DATE**": date, "**COST**": costColumn}))
		if err != nil {
			log.Fatal(err)
		}
		if conf.Metrics[metric].Type == "dimension-per-row" {
			if err := sendMultiDimensionMetric(svc, results, conf.General.Namespace, conf.Metrics[metric].CwName, conf.Metrics[metric].CwType, conf.Metrics[metric].CwDimension); err != nil {
				log.Fatal(err)
			}
		}
	}

}
