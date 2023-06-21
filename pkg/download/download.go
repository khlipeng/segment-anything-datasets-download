package downloade

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/khlipeng/segment-anything-datasets-download/pkg/utils"
	"github.com/schollz/progressbar/v3"
)

type downloader struct {
	taskNum   int
	chunkSize int64
	saveDir   string
	tmpDir    string
}

var saveDir = "./data"
var tmpDir = "./tmp"

func NewDownloader(taskNum int, chunkSize int64, saveDir string, tmpDir string) *downloader {
	return &downloader{
		taskNum:   taskNum,
		chunkSize: chunkSize,
		saveDir:   saveDir,
		tmpDir:    tmpDir,
	}
}

func httpRangeCheck(url string) (int64, error) {
	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Add("Range", "bytes=0-10")
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}

	defer resp.Body.Close()

	hdr, err := ParseHTTPContentRangeHeader(resp.Header.Get("Content-Range"))
	if err != nil {
		return 0, err
	}

	return hdr.ContentLength, nil
}

func (d *downloader) Download(strURL, filename string) error {
	fmt.Println("download", filename)
	if filename == "" {
		filename = path.Base(strURL)
	}

	var contentLength int64 = 0

	resp, err := http.Head(strURL)
	if err != nil {
		fmt.Println(err)
	}
	if err == nil && (resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusPartialContent) && resp.Header.Get("Accept-Ranges") == "bytes" {
		contentLength = resp.ContentLength
	}
	if contentLength == 0 {
		len, err := httpRangeCheck(strURL)
		if err != nil {
			fmt.Println(err)
			return err
		}
		contentLength = len
	}

	if contentLength == 0 {
		return errors.New(filename + " 不支持 Range")
	}

	fmt.Println(filename, "size", contentLength, utils.FormatFileSize(contentLength))
	if info, err := os.Stat(saveDir + "/" + filename); err == nil {
		if info.Size() == contentLength {
			fmt.Printf("%s 文件已经存在\n", filename)
			return nil
		}
		return fmt.Errorf("%s 文件已经存在，但大小不一致,本地文件为 %s，删除后重新下载", filename, utils.FormatFileSize(info.Size()))
	}
	return d.multiDownload(strURL, filename, contentLength)
}

func (d *downloader) multiDownload(strURL, filename string, contentLen int64) error {
	bar := d.newBar(filename, contentLen)
	partNum := int(math.Ceil(float64(contentLen) / float64(d.chunkSize)))
	fmt.Printf("%s 准备下载 partNum: %d\n", filename, partNum)
	bar.RenderBlank()

	// 创建部分文件的存放目录
	partDir := d.getPartDir(filename)
	os.Mkdir(partDir, 0777)
	var wg sync.WaitGroup
	wg.Add(partNum)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// 任务队列
	queue := make(chan int, d.taskNum)
	// 任务重试队列
	redo := make(chan int, d.taskNum)
	go func() {
		for i := 0; i < partNum; i++ {
			queue <- i
		}
	}()

	go func(ctx context.Context) {
		for {
			select {
			case <-ctx.Done():
				return
			// redo是如果某块下载失败了，重新投递到queue，进而重新下载
			case j := <-redo:
				queue <- j
			}
		}
	}(ctx)

	for i := 0; i < d.taskNum; i++ {
		go func(ctx context.Context) {
			for {
				select {
				case <-ctx.Done():
					return
				case i := <-queue:
					// 执行下载
					start := int64(i) * d.chunkSize
					var end int64
					if i < partNum-1 {
						end = start + d.chunkSize - 1
					} else {
						end = contentLen - 1
					}
					err := d.downloadPartial(strURL, filename, start, end, i)
					if err != nil {
						redo <- i
						fmt.Println(filename, i, "err", err)
						continue
					}
					bar.Add(int(end - start))
					wg.Done()
				}
			}
		}(ctx)
	}

	wg.Wait()
	bar.Close()
	err := d.merge(filename, partNum, contentLen)
	if err != nil {
		return err
	}
	os.RemoveAll(d.getPartDir(filename))
	return nil
}

func (d *downloader) downloadPartial(strURL, filename string, rangeStart int64, rangeEnd int64, num int) error {
	if rangeStart >= rangeEnd {
		return nil
	}

	tmpfile := d.getPartFilename(filename, num)
	if info, err := os.Stat(tmpfile); err == nil {
		if info.Size() == rangeEnd-rangeStart+1 {
			return nil
		}
	}

	rangeStr := fmt.Sprintf("bytes=%d-%d", rangeStart, rangeEnd)
	// u, _ := url.Parse(strURL)
	// q := u.Query()
	// q.Set("X-Param-Header-Range", rangeStr)
	// u.RawQuery = q.Encode()

	req, err := http.NewRequest("GET", strURL, nil)
	if err != nil {
		return fmt.Errorf("%s %d 请求错误: %s", filename, num, err)
	}
	req.Header.Set("Range", rangeStr)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s %d 请求错误: %s", filename, num, err)
	}

	defer resp.Body.Close()

	hdr, err := ParseHTTPContentRangeHeader(resp.Header.Get("Content-Range"))
	if err != nil {
		return err
	}

	if hdr.Start != rangeStart || hdr.End != rangeEnd {
		return fmt.Errorf("content range 校验失败: %s  %s", resp.Header.Get("Content-Range"), req.Header.Get("Range"))
	}

	partFile, err := os.OpenFile(tmpfile, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0666)
	if err != nil {
		return fmt.Errorf("%s %d 文件copy异常: openfile error: %s", filename, num, err)
	}
	defer partFile.Close()

	len, err := io.Copy(partFile, resp.Body)
	if err != nil {
		os.Remove(tmpfile)
		return fmt.Errorf("%s %d 文件copy异常: io.copy (%d/%d) error %s", filename, num, len, resp.ContentLength, err)
	}
	if len != rangeEnd-rangeStart+1 {
		os.Remove(tmpfile)
		return fmt.Errorf("%s %d 文件copy异常: 大小不一致", filename, num)
	}
	return nil
}

func (d *downloader) merge(filename string, partNum int, contentLen int64) error {
	filepath := saveDir + "/" + filename
	destFile, err := os.OpenFile(filepath, os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		return err
	}
	defer destFile.Close()

	for i := 0; i < partNum; i++ {
		partFileName := d.getPartFilename(filename, i)
		partFile, err := os.Open(partFileName)
		if err != nil {
			return err
		}
		io.Copy(destFile, partFile)
		partFile.Close()
	}

	info, err := os.Stat(filepath)
	if err != nil {
		return err
	}

	if info.Size() != contentLen {
		return fmt.Errorf("%s 文件合并大小不匹配 %d != %d", filename, info.Size(), contentLen)
	}
	return nil
}

// getPartDir 部分文件存放的目录
func (d *downloader) getPartDir(filename string) string {
	return tmpDir + "/" + strings.ReplaceAll(filename, ".", "_")
}

// getPartFilename 构造部分文件的名字
func (d *downloader) getPartFilename(filename string, partNum int) string {
	partDir := d.getPartDir(filename)
	return fmt.Sprintf("%s/%s-%d", partDir, filename, partNum)
}

func (d *downloader) newBar(filename string, length int64) *progressbar.ProgressBar {
	return progressbar.NewOptions64(
		length,
		progressbar.OptionEnableColorCodes(true),
		progressbar.OptionShowBytes(true),
		progressbar.OptionSetWidth(50),
		progressbar.OptionShowCount(),
		progressbar.OptionThrottle(1*time.Second),
		progressbar.OptionSetDescription(filename),
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        "[green]=[reset]",
			SaucerHead:    "[green]>[reset]",
			SaucerPadding: " ",
			BarStart:      "[",
			BarEnd:        "]",
		}),
	)
}

func ParseHTTPContentRangeHeader(contentRange string) (*ContentRange, error) {
	if len(contentRange) == 0 {
		return nil, fmt.Errorf("parse error:not found Content-Range header")
	}

	contentRange = strings.TrimPrefix(contentRange, "bytes ")

	crs := strings.Split(contentRange, "/")

	if len(crs) != 2 {
		return nil, fmt.Errorf("parse error: Content-Range header: bytes %s", contentRange)
	}

	length, err := strconv.ParseInt(crs[1], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parse error: Content-Range header: %s", contentRange)
	}

	dash := strings.IndexByte(crs[0], '-')
	if dash <= 0 {
		return nil, fmt.Errorf("parse error: Content-Range header: %s", contentRange)
	}

	start, err := strconv.ParseInt(crs[0][:dash], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parse error: Content-Range header: %s", contentRange)
	}
	end, err := strconv.ParseInt(crs[0][dash+1:], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parse error: Content-Range header: %s", contentRange)
	}

	return &ContentRange{
		Start:         start,
		End:           end,
		ContentLength: length,
	}, nil
}

type ContentRange struct {
	Start         int64
	End           int64
	ContentLength int64
}
