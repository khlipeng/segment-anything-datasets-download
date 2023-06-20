package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	downloade "github.com/khlipeng/segment-anything-datasets-download/pkg/download"
	"github.com/khlipeng/segment-anything-datasets-download/pkg/utils"
)

var chunkSize int64 = 1024 * 1024 * 4
var saveDir = "./data"
var tmpDir = "./tmp"

var taskNum = flag.Int("task-num", 10, "task num")
var downloadfile = flag.String("in-file", "./download.txt", "download in file")

func main() {
	flag.Parse()
	fmt.Printf("任务数量:%d ，单任务:%s\n", *taskNum, utils.FormatFileSize(chunkSize))
	file, err := ioutil.ReadFile(*downloadfile)
	if err != nil {
		fmt.Println(err)
		return
	}

	os.Mkdir(saveDir, os.ModePerm)
	os.Mkdir(tmpDir, os.ModePerm)
	downloader := downloade.NewDownloader(*taskNum, chunkSize, saveDir, tmpDir)
	for _, l := range strings.Split(string(file), "\n") {
		var fl []string
		if strings.Index(l, "\t") > 0 {
			fl = strings.Split(l, "\t")
		}
		if strings.Index(l, " ") > 0 {
			fl = strings.Split(l, " ")
		}
		if strings.Index(l, "  ") > 0 {
			fl = strings.Split(l, "  ")
		}
		if len(fl) != 2 {
			fmt.Println("错误的数据", l, len(fl))
			continue
		}
		err := downloader.Download(fl[1], fl[0])
		if err != nil {
			fmt.Println(err)
		}
	}
}
