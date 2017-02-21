package main

import (
	"fmt"
  "log"
  "github.com/BurntSushi/toml"
  "flag"
  "ioutil"
)

type General struct {
  Namespace string
}

type RI struct {
  Enabled int `toml:enableRIanalysis`
  CwName  string
  CwDimension string
  CwUnit string
}

type Athena struct {
  DbSQL string `toml:create_database`
  TableSQL string `toml:create_table`
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

var defaultConfigPath = "./analyzeDBR.config"


func getConfig(conf *Config) error {
    var configFile string

    // Define input commnad line config parameter and parse it
    flag.StringVar(&configFile, "c", defaultConfigPath, "Input config file for analyzeDBR")
    flag.Parse()

    // check for existance of file
    if _, err := os.Stat(configFile); err != nil {
      return errors.New("Config File " + configFile + " does not exist")
    }

    // read file
    if b, err := ioutil.ReadFile(configFile); err != nil {
      return err
    }

    // parse TOML config file into struct
    if _, err := toml.Decode(b, &conf); err != nil {
      return err
    }

    return nil
}



func main() {

  var conf Config
  if err := getConfig(&conf); err != nil {
    log.Fatal(err)
  }

  fmt.Println("done")
}
