package main

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"

	nagios "source.datanerd.us/site-engineering/go_nagios"
)

const (
	API_VERSION = "v1.10"
)

type CliOpt struct {
	BaseUrl string
	CritDataSpace int
	WarnDataSpace int
	CritMetaSpace int
	WarnMetaSpace int
}

type DockerInfo struct {
	Containers     float64
	DriverStatus   [][]string
	DataSpaceUsed  float64
	DataSpaceTotal float64
	MetaSpaceUsed  float64
	MetaSpaceTotal float64
}

type HttpResponseFetcher interface {
	Fetch(url string) ([]byte, error)
}

type Fetcher struct{}

func float64String(num float64) string {
	return strconv.FormatFloat(num, 'f', 0, 64)
}

func megabytesFloat64(value string) (float64, error) {
	numberStr   := strings.Fields(value)[0]
	number, err := strconv.ParseFloat(numberStr, 64)

	if err != nil {
		return 0.00, err
	}

	return number, nil
}

// Look through a list of driveStatus slices and find the one that matches
func findDriverStatus(key string, driverStatus [][]string) string {
	for _, entry := range(driverStatus) {
		if entry[0] == key {
			return entry[1]
		}
	}

	return ""
}

// Connect to a Docker URL and return the contents as a []byte
func (Fetcher) Fetch(url string) ([]byte, error) {
	response, err := http.Get(url + "/" + API_VERSION + "/info")

	if err != nil {
		return nil, err
	}

	defer response.Body.Close()

	contents, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	return contents, nil
}

func populateInfo(contents []byte, info *DockerInfo) error {
	err := json.Unmarshal(contents, &info)
	if err != nil {
		return err
	}

	fields := map[string]*float64{
		"Data Space Used":      &info.DataSpaceUsed,
		"Data Space Total":     &info.DataSpaceTotal,
		"Metadata Space Used":  &info.MetaSpaceUsed,
		"Metadata Space Total": &info.MetaSpaceTotal,
	}

	for key, val := range(fields) {
	  *val, err = megabytesFloat64(findDriverStatus(key, info.DriverStatus))
	  if err != nil {
		return errors.New("Error parsing response from API! " + err.Error())
	  }
	}

	return nil
}

func fetchInfo(fetcher Fetcher, opts CliOpt, info *DockerInfo) error {
	contents, err := fetcher.Fetch(opts.BaseUrl)
	if err != nil {
		return err
	}

	err = populateInfo(contents, info)
	if err != nil {
		return err
	}

	return nil
}

func mapAlertStatuses(info *DockerInfo, opts *CliOpt) []*nagios.NagiosStatus {
	var statuses []*nagios.NagiosStatus

	type checkArgs struct {
		tag string
		value float64
		comparison float64
		statusVal nagios.NagiosStatusVal
	}

	var check = func(args checkArgs) *nagios.NagiosStatus {
		if args.value > args.comparison {
			return &nagios.NagiosStatus{args.tag + ": " + float64String(args.value), args.statusVal}
		}
		return nil
	}

	checks := []checkArgs{
		checkArgs{"Meta Space Used",
			info.MetaSpaceUsed / info.MetaSpaceTotal * 100,
			float64(opts.CritMetaSpace),
			nagios.NAGIOS_CRITICAL,
		},
		checkArgs{"Data Space Used",
			info.DataSpaceUsed / info.DataSpaceTotal * 100,
			float64(opts.CritDataSpace),
			nagios.NAGIOS_CRITICAL,
		},
		checkArgs{"Meta Space Used",
			info.MetaSpaceUsed / info.MetaSpaceTotal * 100,
			float64(opts.WarnMetaSpace),
			nagios.NAGIOS_WARNING,
		},
		checkArgs{"Data Space Used",
			info.DataSpaceUsed / info.DataSpaceTotal * 100,
			float64(opts.WarnDataSpace),
			nagios.NAGIOS_WARNING,
		},
	}

	for _, entry := range(checks) {
		result := check(entry)
		if result != nil {
			statuses = append(statuses, check(entry))
		}
	}

	return statuses
}

func main() {
	opts := CliOpt{
		BaseUrl:      "http://chi-staging-pool-1:4243/",
		WarnMetaSpace: 0,
		WarnDataSpace: 0,
		CritMetaSpace: 66,
		CritDataSpace: 66,
	}

	var fetcher Fetcher
	var info DockerInfo

	err := fetchInfo(fetcher, opts, &info)
	if err != nil {
		nagios.Critical(err)
	}

	statuses := mapAlertStatuses(&info, &opts)
	status   := nagios.NagiosStatus{float64String(info.Containers) + " containers", 0}

	status.Aggregate(statuses)
	nagios.ExitWithStatus(&status)
}