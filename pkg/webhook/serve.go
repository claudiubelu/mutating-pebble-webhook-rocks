/*
Copyright 2024 Canonical, Ltd.

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

package webhook

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	v1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func toV1AdmissionResponse(err error) *v1.AdmissionResponse {
	return &v1.AdmissionResponse{
		Result: &metav1.Status{
			Message: err.Error(),
		},
	}
}

func deserializeRequest(req *http.Request) (*v1.AdmissionReview, error) {
	contentType := req.Header.Get("Content-Type")
	if contentType != "application/json" {
		return nil, fmt.Errorf("Content-Type: '%s', expected: application/json", contentType)
	}

	buff := new(bytes.Buffer)
	if _, err := buff.ReadFrom(req.Body); err != nil {
		return nil, fmt.Errorf("couldn't read request body: %w", err)
	}
	body := buff.Bytes()

	if len(body) == 0 {
		return nil, fmt.Errorf("empty admission request body")
	}

	requestedReview := v1.AdmissionReview{}
	_, gvk, err := deserializer.Decode(body, nil, &requestedReview)
	if err != nil {
		return nil, fmt.Errorf("request could not be decoded: %w", err)
	}

	if *gvk != v1.SchemeGroupVersion.WithKind("AdmissionReview") {
		return nil, fmt.Errorf("unsupported group version kind: %v", gvk)
	}

	if requestedReview.Request == nil {
		return nil, fmt.Errorf("invalid admission review: Request field is nil")
	}

	return &requestedReview, nil
}

func ServeAddPebbleMount(w http.ResponseWriter, req *http.Request) {
	logger := slog.Default().With("URI", req.RequestURI)
	logger.Info("Mutating Pod...")

	request, err := deserializeRequest(req)
	if err != nil {
		logger.Error("Encountered error while deserializing.", "error", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	response, err := addPebbleMountMutation(request)
	if err != nil {
		logger.Error("Encountered error while processing request.", "error", err)
		response = toV1AdmissionResponse(err)
	}

	resp := &v1.AdmissionReview{}
	resp.SetGroupVersionKind(v1.SchemeGroupVersion.WithKind("AdmissionReview"))
	resp.Response = response
	resp.Response.UID = request.Request.UID

	logger.Info("Sending response.", "response", resp)
	respBytes, err := json.Marshal(resp)
	if err != nil {
		logger.Error("Encountered error while marshaling response.", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(respBytes); err != nil {
		logger.Error("Encountered error while writing response.", "error", err)
	}
}

func ServeHealthz(w http.ResponseWriter, _ *http.Request) {
	slog.Debug("Healthy")
	if _, err := w.Write([]byte("OK")); err != nil {
		slog.Error("Encountered error while reporting health.", "error", err)
	}
}
