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

package poller

import (
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/turbinelabs/api"
	"github.com/turbinelabs/api/service"
	"github.com/turbinelabs/test/assert"
)

func TestRegistrar(t *testing.T) {
	reps := 10000

	ctrl := gomock.NewController(assert.Panicking(assert.Tracing(t)))
	defer ctrl.Finish()

	proxy := api.Proxy{Name: "that-proxy"}

	svc := service.NewMockAll(ctrl)
	pRefs := []*service.MockProxyRef{
		service.NewMockProxyRef(ctrl),
		service.NewMockProxyRef(ctrl),
		service.NewMockProxyRef(ctrl),
	}

	for i := range pRefs {
		k := fmt.Sprintf("that-proxy-%d", i)
		pRefs[i].EXPECT().Get(svc).Return(proxy, nil).MinTimes(0).MaxTimes(reps)
		pRefs[i].EXPECT().MapKey().Return(k).MinTimes(0).MaxTimes(2 * reps)
	}

	firsts := 0
	ifFirst := func(gotSvc service.All, gotProxy api.Proxy) {
		assert.Equal(t, gotSvc, svc)
		assert.DeepEqual(t, gotProxy, proxy)
		firsts++
	}

	lasts := 0
	ifLast := func() {
		lasts++
	}

	reg := NewRegistrar(svc)

	var wg sync.WaitGroup

	i := 0
	for i < reps {
		i++

		j := rand.Intn(2)

		pRef := pRefs[j]

		wg.Add(1)
		go func() {
			err := reg.Register(pRef, ifFirst)
			assert.Nil(t, err)
			refs := reg.Refs()

			l := len(refs)

			assert.GreaterThan(t, l, 0)
			assert.LessThan(t, l, 3)

			go func() {
				err := reg.Deregister(pRef, ifLast)
				assert.Nil(t, err)
				wg.Done()
			}()
		}()
	}

	wg.Wait()
	assert.GreaterThan(t, firsts, 0)
	assert.Equal(t, firsts, lasts)
}

func TestRegistrarRegisterGetFails(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	svc := service.NewMockAll(ctrl)
	pRef := service.NewMockProxyRef(ctrl)
	pRef.EXPECT().Get(svc).Return(api.Proxy{}, errors.New("boom"))

	reg := NewRegistrar(svc)
	err := reg.Register(pRef, nil)
	assert.ErrorContains(t, err, "boom")
}

func TestRegistrarDeregisterNotRegistered(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	svc := service.NewMockAll(ctrl)
	pRef := service.NewMockProxyRef(ctrl)
	pRef.EXPECT().MapKey().Return("that-proxy")

	reg := NewRegistrar(svc)
	err := reg.Deregister(pRef, nil)
	assert.ErrorContains(t, err, `deregister attempt on unregistered proxy: "that-proxy"`)
}
