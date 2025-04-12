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

package dao

import (
	"fmt"

	"dingo-hfmirror/pkg/common"
	"dingo-hfmirror/pkg/config"
	"dingo-hfmirror/pkg/consts"
	"dingo-hfmirror/pkg/util"

	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

type MetaDao struct {
	fileDao *FileDao
}

func NewMetaDao(fileDao *FileDao) *MetaDao {
	return &MetaDao{
		fileDao: fileDao,
	}
}

func (m *MetaDao) MetaGetGenerator(c echo.Context, repoType, org, repo, commit, method string) error {
	orgRepo := util.GetOrgRepo(org, repo)
	apiDir := fmt.Sprintf("%s/api/%s/%s/revision/%s", config.SysConfig.Repos(), repoType, orgRepo, commit)
	apiMetaPath := fmt.Sprintf("%s/%s", apiDir, fmt.Sprintf("meta_%s.json", method))
	err := util.MakeDirs(apiMetaPath)
	if err != nil {
		zap.S().Errorf("create %s dir err.%v", apiDir, err)
		return err
	}
	request := c.Request()
	authorization := request.Header.Get("authorization")
	// 若缓存文件存在，且为离线模式，从缓存读取
	if util.FileExists(apiMetaPath) && !config.SysConfig.Online() {
		return m.MetaCacheGenerator(c, repo, apiMetaPath)
	} else {
		return m.MetaProxyGenerator(c, repoType, org, repo, commit, method, authorization, apiMetaPath)
	}
}

func (m *MetaDao) MetaCacheGenerator(c echo.Context, repo, apiMetaPath string) error {
	cacheContent, err := m.fileDao.ReadCacheRequest(apiMetaPath)
	if err != nil {
		return err
	}
	var bodyStreamChan = make(chan []byte, consts.RespChanSize)
	bodyStreamChan <- cacheContent.OriginContent
	close(bodyStreamChan)
	err = util.ResponseStream(c, repo, cacheContent.Headers, bodyStreamChan)
	if err != nil {
		return err
	}
	return nil
}

// 请求api文件

func (m *MetaDao) MetaProxyGenerator(c echo.Context, repoType, org, repo, commit, method, authorization, apiMetaPath string) error {
	orgRepo := util.GetOrgRepo(org, repo)
	metaUrl := fmt.Sprintf("%s/api/%s/%s/revision/%s", config.SysConfig.GetHFURLBase(), repoType, orgRepo, commit)
	headers := map[string]string{}
	if authorization != "" {
		headers["authorization"] = authorization
	}
	if method == consts.RequestTypeHead {
		resp, err := util.RetryRequest(func() (*common.Response, error) {
			return util.Head(metaUrl, headers, config.SysConfig.GetReqTimeOut())
		})
		if err != nil {
			zap.S().Errorf("head %s err.%v", metaUrl, err)
			return util.ErrorEntryNotFound(c)
		}
		extractHeaders := resp.ExtractHeaders(resp.Headers)
		return util.ResponseHeaders(c, extractHeaders)
	} else if method == consts.RequestTypeGet {
		resp, err := util.RetryRequest(func() (*common.Response, error) {
			return util.Get(metaUrl, headers, config.SysConfig.GetReqTimeOut())
		})
		if err != nil {
			zap.S().Errorf("get %s err.%v", metaUrl, err)
			return util.ErrorEntryNotFound(c)
		}
		extractHeaders := resp.ExtractHeaders(resp.Headers)
		var bodyStreamChan = make(chan []byte, consts.RespChanSize)
		bodyStreamChan <- resp.Body
		close(bodyStreamChan)
		err = util.ResponseStream(c, repo, extractHeaders, bodyStreamChan)
		if err != nil {
			return err
		}
		if err = m.fileDao.WriteCacheRequest(apiMetaPath, resp.StatusCode, extractHeaders, resp.Body); err != nil {
			zap.S().Errorf("writeCacheRequest err.%v", err)
			return nil
		}
	}
	return nil
}
