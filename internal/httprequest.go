package internal

import (
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/huobirdcenter/huobi_golang/logging/applogger"
	"github.com/huobirdcenter/huobi_golang/logging/perflogger"
)

func HttpGet(url string) (string, error) {
	logger := perflogger.GetInstance()
	logger.Start()

	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	result, err := ioutil.ReadAll(resp.Body)

	logRateLimit(resp)

	logger.StopAndLog("GET", url)

	return string(result), err
}

func HttpPost(url string, body string) (string, error) {
	logger := perflogger.GetInstance()
	logger.Start()

	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	result, err := ioutil.ReadAll(resp.Body)

	logRateLimit(resp)
	logger.StopAndLog("POST", url)

	return string(result), err
}

var counter = 0

func logRateLimit(resp *http.Response) {
	counter += 1
	// just log, do not need precise here
	if counter%100 == 0 {
		applogger.Debug("Huobi RateLimit Remain: %s, Expire: %s",
			resp.Header.Get("X-HB-RateLimit-Requests-Remain"), resp.Header.Get("X-HB-RateLimit-Requests-Expire"))
	}
}
