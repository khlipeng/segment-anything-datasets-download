package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
)

var (
	listURL     = "https://opendatalab.org.cn/api/datasets/6248/files?pageSize=1000&pageNo=%d&prefix=raw"
	downloadURL = "https://opendatalab.org.cn/api/track/datasets/download/6248"
)

var savePATH = flag.String("save", "./download.txt", "save download list file")

func main() {
	flag.Parse()
	list := make([]OpenDataLabFileInfo, 0)
	page := 2
	for i := 1; i <= page; i++ {
		resp, err := http.Get(fmt.Sprintf(listURL, i))
		if err != nil {
			panic(err)
		}

		defer resp.Body.Close()

		datalist := &OpenDataLabDataList{}
		err = json.NewDecoder(resp.Body).Decode(datalist)
		if err != nil {
			panic(err)
		}
		if datalist.Code != 200 {
			panic(datalist.Msg)
		}
		for _, v := range datalist.Data.List {
			v.Name = "raw/" + v.Path
			list = append(list, v)
		}
	}

	buf := bytes.NewBuffer(nil)
	err := json.NewEncoder(buf).Encode(list)
	if err != nil {
		panic(err)
	}

	req, err := http.NewRequest("POST", downloadURL, buf)
	if err != nil {
		panic(err)
	}
	req.Header.Set("Content-Type", "application/json")
	cookie := os.Getenv("OPENDATALAB_COOKIE")
	req.Header.Set("Cookie", cookie)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	downloadResp := &OpenDataLabDownloadDataList{}

	err = json.NewDecoder(resp.Body).Decode(downloadResp)
	if err != nil {
		panic(err)
	}

	if downloadResp.Code != 200 {
		panic(downloadResp.Msg)
	}

	file, err := os.OpenFile(*savePATH, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0666)
	if err != nil {
		panic(err)
	}

	defer file.Close()
	for _, info := range downloadResp.Data {
		file.WriteString(fmt.Sprintf("%s\t%s\n", strings.ReplaceAll(info.Name, "raw/", ""), info.URL))
	}
}

type OpenDataLabDataList struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		List  []OpenDataLabFileInfo `json:"list"`
		Total int                   `json:"total"`
	} `json:"data"`
}

type OpenDataLabFileInfo struct {
	Path  string `json:"path"`
	IsDir bool   `json:"isDir"`
	Name  string `json:"name"`
	Size  int    `json:"size"`
}

type OpenDataLabDownloadDataList struct {
	Code int                       `json:"code"`
	Msg  string                    `json:"msg"`
	Data []OpenDataLabDownloadInfo `json:"data"`
}

type OpenDataLabDownloadInfo struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}
