//  Copyright (c) 2025 dingodb.com, Inc. All Rights Reserved
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http:www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package downloader

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"dingo-hfmirror/pkg/common"
	"dingo-hfmirror/pkg/config"
	"dingo-hfmirror/pkg/util"

	"go.uber.org/zap"
)

type Downloader struct {
	common.FileMetadata                     // 文件元数据
	waitGoroutine       sync.WaitGroup      // 同步goroutine
	DownloadDir         string              // 下载文件保存目录
	RetryChannel        chan common.Segment // 重传channel通道
	MaxGtChannel        chan struct{}       // 限制上传的goroutine的数量通道
	StartTime           int64               // 下载开始时间
	DownloadUrl         string              // 文件下载路径
	IsOpen              bool                // 文件是否被打开
	BlockData           chan []byte         // 块文件数据输出
	NextBlock           chan int            // 发送下一个块文件
}

type FileInfo struct {
	FileName string
	FileSize int64
}

func GetDownLoader(fileInfo FileInfo, downloadDir string) (*Downloader, error) {
	// downloadingFile := getDownloadMetaFile(path.Join(downloadDir, fileInfo.FileName))
	// 检查下载文件是否存在，若存在表示是上次未下载完成的文件
	// if util.IsFile(downloadingFile) {
	// 	loader := &Downloader{
	// 		DownloadDir:  downloadDir,
	// 		RetryChannel: make(chan common.Segment, config.SysConfig.Download.RetryChannelNum),
	// 		MaxGtChannel: make(chan struct{}, config.SysConfig.Download.GoroutineMaxNumPerFile),
	// 		StartTime:    time.Now().Unix(),
	// 	}
	//
	// 	file, err := os.Open(downloadingFile)
	// 	if err != nil {
	// 		fmt.Println("获取文件状态失败")
	// 		return nil, err
	// 	}
	// 	var metadata common.FileMetadata
	// 	filedata := gob.NewDecoder(file)
	// 	err = filedata.Decode(&metadata)
	// 	if err != nil {
	// 		fmt.Println("格式化文件数据失败")
	// 	}
	// 	loader.FileMetadata = metadata
	// 	// 计算还需下载的分片
	// 	// sliceseq, err := loader.calNeededSlice()
	// 	// if err != nil {
	// 	//	os.Remove(downloadingFile)
	// 	//	return nil
	// 	// }
	// 	return loader, nil
	// } else {
	// 没有downloading文件，重新下载
	return NewDownLoader(fileInfo, downloadDir)
	// }
}

// NewDownLoader 新建一个下载器
func NewDownLoader(fileInfo FileInfo, downloadDir string) (*Downloader, error) {
	var metadata common.FileMetadata
	metadata.Fid = util.UUID()
	metadata.Filesize = fileInfo.FileSize
	metadata.Filename = fileInfo.FileName
	count, segments := util.SplitFileToSegment(fileInfo.FileSize, config.SysConfig.Download.BlockSize)
	metadata.SliceNum = count
	metadata.Segments = segments

	// 创建下载分片保存路径文件夹
	dSliceDir := getSliceDir(path.Join(downloadDir, fileInfo.FileName), metadata.Fid)
	err := os.MkdirAll(dSliceDir, 0766)
	if err != nil {
		zap.S().Error("创建下载分片目录失败", dSliceDir, err)
		return nil, err
	}
	metadataPath := getDownloadMetaFile(path.Join(downloadDir, fileInfo.FileName))
	err = util.StoreMetadata(metadataPath, &metadata)
	if err != nil {
		zap.S().Error("StoreMetadata err.%S,%v", metadataPath, err)
		return nil, err
	}
	return &Downloader{
		DownloadDir:  downloadDir,
		FileMetadata: metadata,
		RetryChannel: make(chan common.Segment, config.SysConfig.Download.RetryChannelNum),
		MaxGtChannel: make(chan struct{}, config.SysConfig.Download.GoroutineMaxNumPerFile),
		StartTime:    time.Now().Unix(),
		BlockData:    make(chan []byte, 5),
		NextBlock:    make(chan int, 5),
	}, nil
}

// 获取上传元数据文件路径
func getDownloadMetaFile(filePath string) string {
	paths, fileName := filepath.Split(filePath)
	return path.Join(paths, "."+fileName+".downloading")
}

func getSliceDir(filePath string, fid string) string {
	paths, _ := filepath.Split(filePath)
	return path.Join(paths, fid)
}

// 计算还需下载的分片序号
func (d *Downloader) calNeededSlice() ([]*common.Segment, error) {
	// initialSegments := d.Segments
	//
	// // 获取已下载的文件片序号
	// storeSeq := make(map[string]bool)
	// files, _ := ioutil.ReadDir(getSliceDir(path.Join(d.DownloadDir, d.Filename), d.Fid))
	// for _, file := range files {
	//	_, err := strconv.Atoi(file.Name())
	//	if err != nil {
	//		fmt.Println("文件片有错", err, file.Name())
	//		continue
	//	}
	//	storeSeq[file.Name()] = true
	// }
	//
	// i := 0
	// for ; i < d.SliceNum && len(storeSeq) > 0; i++ {
	//	indexStr := strconv.Itoa(i)
	//	if _, ok := storeSeq[indexStr]; ok {
	//		delete(storeSeq, indexStr)
	//	} else {
	//		seq.Slices = append(seq.Slices, i)
	//	}
	// }
	//
	// // -1指代slices的最大数字序号到最后一片都没有收到
	// if i < d.SliceNum {
	//	seq.Slices = append(seq.Slices, i)
	//	i += 1
	//	if i < d.SliceNum {
	//		seq.Slices = append(seq.Slices, -1)
	//	}
	// }
	//
	// fmt.Printf("%s还需重新下载的片\n", d.Filename)
	// fmt.Println(seq.Slices)
	// return &seq, nil
	return nil, nil
}

// DownloadFile 单个文件的下载
func (d *Downloader) DownloadFile() error {
	if !util.IsDir(d.DownloadDir) {
		fmt.Printf("指定下载路径：%s 不存在\n", d.DownloadDir)
		return errors.New("指定下载路径不存在")
	}

	client := http.Client{}
	req, err := http.NewRequest(http.MethodGet, d.DownloadUrl, nil)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	filePath := path.Join(d.DownloadDir, d.Filename)
	f, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE, 0666)
	if err != nil {
		fmt.Printf(err.Error())
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	if err != nil {
		return err
	}
	fmt.Printf("%s 文件下载成功，保存路径：%s\n", d.Filename, filePath)
	return nil
}

// DownloadFileBySlice 切片方式下载文件
func (d *Downloader) DownloadFileBySlice() error {
	// 启动重下载goroutine
	go d.retryDownloadSlice()
	go d.SendNextBlock()
	metadata := &d.FileMetadata
	for _, segment := range metadata.Segments {
		tmp := segment
		d.waitGoroutine.Add(1)
		go d.downloadSlice(tmp)
	}
	// 等待各个分片都下载完成了
	zap.S().Infof("%s启动分片下载\n", d.Filename)
	d.waitGoroutine.Wait()
	zap.S().Infof("%s分片都已下载完成\n", d.Filename)
	return nil
}

// 重下载失败的分片
func (d *Downloader) retryDownloadSlice() {
	for segment := range d.RetryChannel {
		// 检查下载是否超时了
		if time.Now().Unix()-d.StartTime > config.SysConfig.Download.Timeout {
			fmt.Println("下载超时，请重试")
			d.waitGoroutine.Done()
		}
		fmt.Printf("重下载文件分片，文件名:%s, 分片序号:%d\n", d.Filename, segment)
		go d.downloadSlice(&segment)
	}
}

// 重下载失败的分片
func (d *Downloader) SendNextBlock() error {
	for {
		index, ok := <-d.NextBlock
		if !ok {
			return nil
		}
		// 先判断文件划分了多少块，若只有1块，就直接关闭退出。
		filePath := path.Join(getSliceDir(path.Join(d.DownloadDir, d.Filename), d.Fid), strconv.Itoa(index))
		for !util.FileExists(filePath) {
			waitTime := config.SysConfig.Download.WaitNextBlockTime
			zap.S().Infof("file is not exist，need to wait %d second", waitTime)
			time.Sleep(time.Duration(waitTime) * time.Second)
		}
		sliceFile, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE, 0666)
		if err != nil {
			return err
		}
		b, err := ioutil.ReadAll(sliceFile)
		if err != nil {
			return err
		}
		d.BlockData <- b
		index++
		if index == len(d.Segments) { // 这是最后一个segment
			close(d.BlockData)
			close(d.NextBlock)
		} else {
			d.NextBlock <- index
		}
	}
}

// 下载分片
func (d *Downloader) downloadSlice(segment *common.Segment) error {
	d.MaxGtChannel <- struct{}{}
	defer func() {
		<-d.MaxGtChannel
	}()
	client := http.Client{}
	req, err := http.NewRequest(http.MethodGet, d.DownloadUrl, nil)
	if err != nil {
		return err
	}
	req.Header.Add("range", fmt.Sprintf("bytes=%d-%d", segment.Start, segment.End))
	resp, err := client.Do(req)
	if err != nil {
		zap.S().Errorf("client do err.range:%d-%d,%v", segment.Start, segment.End, err)
		d.RetryChannel <- *segment
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		zap.S().Infof("resp statuscode:%d", resp.StatusCode)
		d.RetryChannel <- *segment
		errMsg, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			zap.S().Errorf("resp body read err,%v", err)
			return err
		}
		return errors.New(string(errMsg))
	}

	filePath := path.Join(getSliceDir(path.Join(d.DownloadDir, d.Filename), d.Fid), strconv.Itoa(segment.Index))
	sliceFile, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE, 0666)
	if err != nil {
		zap.S().Errorf("open file %s err,%v", filePath, err)
		d.RetryChannel <- *segment
		return err
	}

	// 写入cache，需要先写入文件落盘，才能写cache，避免文件写入失败导致数据不一致。
	// key := GetFileBlockKey(d.DownloadDir, d.Filename, segment.Index)
	// cache.FileBlockCache.Set(key, bodyBytes, 1)

	// 将第一个块输出发送到用户侧
	if segment.Index == 0 {
		// buffer := make([]byte, 10000)
		// _, err = io.Copy(bytes.NewBuffer(buffer), resp.Body)
		// n, err := resp.Body.Read(buffer)
		// 写入文件
		// _, err = sliceFile.Write(buffer)
		_, err = io.Copy(sliceFile, resp.Body)
		if err != nil {
			zap.S().Errorf("文件%s的%d分片拷贝失败，失败原因:%s\n", d.Filename, segment.Index, err.Error())
			d.RetryChannel <- *segment
			return err
		}
		sliceFile.Close()

		buffer, err := util.ReadFileToBytes(filePath)
		if err != nil {
			zap.S().Errorf("open file %s err,%v", filePath, err)
			return err
		}

		d.BlockData <- buffer
		index := segment.Index + 1
		if index == len(d.Segments) { // 这是最后一个segment
			close(d.BlockData)
			close(d.NextBlock)
		} else {
			d.NextBlock <- index
		}
	} else {
		_, err = io.Copy(sliceFile, resp.Body)
		if err != nil {
			zap.S().Errorf("文件%s的%d分片拷贝失败，失败原因:%s\n", d.Filename, segment.Index, err.Error())
			d.RetryChannel <- *segment
			return err
		}
		sliceFile.Close()
	}
	d.waitGoroutine.Done()
	return nil
}

func GetFileBlockKey(repoPath, fileName string, index int) string {
	return util.Md5(fmt.Sprintf("%s/%s/%d", repoPath, fileName, index))
}

// MergeDownloadFiles 合并分片文件为一个文件
func (d *Downloader) MergeDownloadFiles() error {
	fmt.Println("开始合并文件", d.Filename)
	targetFile := path.Join(d.DownloadDir, d.Filename)
	realFile, err := os.OpenFile(targetFile, os.O_WRONLY|os.O_CREATE, 0666)
	if err != nil {
		fmt.Println(err)
		return err
	}
	sliceDir := getSliceDir(path.Join(d.DownloadDir, d.Filename), d.Fid)
	// 计算md5值，这里要注意，一定要按分片顺序计算，不要使用读目录文件的方式，返回的文件顺序是无保证的
	// md5hash := md5.New()
	defer os.Remove(getDownloadMetaFile(targetFile))
	defer os.RemoveAll(sliceDir)
	for i := 0; i < d.SliceNum; i++ {
		sliceFilePath := path.Join(sliceDir, strconv.Itoa(i))
		sliceFile, err := os.Open(sliceFilePath)
		if err != nil {
			fmt.Printf("读取文件%s失败, err: %s\n", sliceFilePath, err)
			return err
		}
		// 偏移量需要重新进行调整
		io.Copy(realFile, sliceFile)
		sliceFile.Close()
	}
	realFile.Close()
	fmt.Printf("%s文件下载成功，保存路径：%s\n", d.Filename, targetFile)
	return nil
}
