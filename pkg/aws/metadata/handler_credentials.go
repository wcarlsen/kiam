// Copyright 2017 uSwitch
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/gorilla/mux"
	"github.com/rcrowley/go-metrics"
	"github.com/uswitch/kiam/pkg/aws/sts"
	"github.com/uswitch/kiam/pkg/k8s"
	"net/http"
	"time"
)

type credentialsHandler struct {
	roleFinder          k8s.RoleFinder
	credentialsProvider sts.CredentialsProvider
	clientIP            clientIPFunc
}

func (c *credentialsHandler) Handle(ctx context.Context, w http.ResponseWriter, req *http.Request) (int, error) {
	credentialTimings := metrics.GetOrRegisterTimer("credentialsHandler", metrics.DefaultRegistry)
	startTime := time.Now()
	defer credentialTimings.UpdateSince(startTime)

	err := req.ParseForm()
	if err != nil {
		return http.StatusInternalServerError, err
	}

	ip, err := c.clientIP(req)
	if err != nil {
		return http.StatusInternalServerError, err
	}

	foundRole, err := findRole(ctx, c.roleFinder, ip)
	if err != nil {
		metrics.GetOrRegisterMeter("credentialsHandler.findRoleError", metrics.DefaultRegistry).Mark(1)
		return http.StatusInternalServerError, fmt.Errorf("error finding pod for ip %s: %s", ip, err.Error())
	}

	if foundRole == "" {
		metrics.GetOrRegisterMeter("credentialsHandler.emptyRole", metrics.DefaultRegistry).Mark(1)
		return http.StatusNotFound, EmptyRoleError
	}

	requestedRole := mux.Vars(req)["role"]
	if requestedRole == "" {
		return http.StatusBadRequest, fmt.Errorf("no role specified")
	}

	if foundRole != requestedRole {
		return http.StatusForbidden, fmt.Errorf("unable to assume role %s, role on pod specified is %s", requestedRole, foundRole)
	}

	credentials, err := credentialsForRole(ctx, c.credentialsProvider, requestedRole)
	if err != nil {
		return http.StatusInternalServerError, fmt.Errorf("unexpected error: %s", ctx.Err().Error())
	}

	err = json.NewEncoder(w).Encode(credentials)
	if err != nil {
		return http.StatusInternalServerError, fmt.Errorf("error encoding credentials: %s", err.Error())
	}

	w.Header().Set("Content-Type", "application/json")
	metrics.GetOrRegisterMeter("credentialsHandler.success", metrics.DefaultRegistry).Mark(1)
	return http.StatusOK, nil
}
