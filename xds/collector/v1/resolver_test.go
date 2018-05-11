/*
Copyright 2018 Turbine Labs, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

import (
	"bytes"
	"errors"
	"io/ioutil"
	"net/http"
	"testing"

	"github.com/turbinelabs/test/assert"
)

func TestClusterResolverErrorOnEmptyClusterName(t *testing.T) {
	cr := NewClusterResolver("url", 1, func(string) (*http.Response, error) {
		return nil, nil
	})

	is, errs := cr("")
	assert.Nil(t, is)
	assert.NonNil(t, errs)
}

func TestClusterResolverClientErrorsPassedThrough(t *testing.T) {
	cr := NewClusterResolver("url", 1, func(string) (*http.Response, error) {
		return nil, errors.New("bleep")
	})
	is, errs := cr("service-name")
	assert.Nil(t, is)
	assert.NonNil(t, errs)
}

func TestClusterResolverCodecErrorsPassedThrough(t *testing.T) {
	cr := NewClusterResolver("url", 1, func(url string) (*http.Response, error) {
		return &http.Response{
			Status:     "200 OK",
			StatusCode: http.StatusOK,
			Body:       ioutil.NopCloser(bytes.NewBufferString("nope")),
		}, nil
	})
	is, errs := cr("service-name")
	assert.Nil(t, is)
	assert.NonNil(t, errs)
}

func TestClusterResolverNon200ResponsesError(t *testing.T) {
	cr := NewClusterResolver("url", 1, func(url string) (*http.Response, error) {
		return &http.Response{
			Status:     "404 NOT FOUND",
			StatusCode: http.StatusNotFound,
			Body:       ioutil.NopCloser(bytes.NewBufferString("nope")),
		}, nil
	})
	is, errs := cr("service-name")
	assert.Nil(t, is)
	assert.NonNil(t, errs)
}
