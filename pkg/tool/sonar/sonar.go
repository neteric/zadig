/*
Copyright 2021 The KodeRover Authors.

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

package sonar

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/viper"

	vmlog "github.com/koderover/zadig/v2/pkg/cli/zadig-agent/helper/log"
	"github.com/koderover/zadig/v2/pkg/tool/httpclient"
	"github.com/koderover/zadig/v2/pkg/tool/log"
)

type Client struct {
	*httpclient.Client

	host  string
	token string
}

const (
	SonarWorkDirKey = "sonar.working.directory"
	CETaskIDKey     = "ceTaskId"
	ProjectKey      = "projectKey"
	BranchKey       = "branch"
)

func NewSonarClient(host, token string) *Client {
	encodeToken := base64.StdEncoding.EncodeToString([]byte(token + ":"))
	c := httpclient.New(
		httpclient.SetAuthToken(encodeToken),
		httpclient.SetAuthScheme("Basic"),
		httpclient.SetHostURL(host),
	)

	return &Client{
		Client: c,
		host:   host,
		token:  token,
	}
}

type CETaskStatus string

const (
	CETaskPending    CETaskStatus = "PENDING"
	CETaskInProgress CETaskStatus = "IN_PROGRESS"
	CETaskSuccess    CETaskStatus = "SUCCESS"
	CETaskFailed     CETaskStatus = "FAILED"
	CETaskCanceled   CETaskStatus = "CANCELED"
)

type CETaskInfo struct {
	Task CETask `json:"task"`
}

type CETask struct {
	ID             string       `json:"id"`
	Type           string       `json:"type"`
	ComponentID    string       `json:"componentId"`
	ComponentKey   string       `json:"componentKey"`
	AnalysisID     string       `json:"analysisId"`
	Status         CETaskStatus `json:"status"`
	SubmitterLogin string       `json:"submitterLogin"`
	WarningCount   int          `json:"warningCount"`
	SubmittedAt    string       `json:"submittedAt"`
	StartedAt      string       `json:"startedAt"`
	ExecutedAt     string       `json:"executedAt"`
}

type MeasuresComponentResponse struct {
	Component Component `json:"component"`
}

type Component struct {
	Measures []Measure `json:"measures"`
}

type Measure struct {
	Metric string `json:"metric"`
	Value  string `json:"value"`
}

func (c *Client) GetCETaskInfo(taskID string) (*CETaskInfo, error) {
	url := "/api/ce/task"
	res := &CETaskInfo{}
	if _, err := c.Client.Get(url, httpclient.SetQueryParam("id", taskID), httpclient.SetResult(res)); err != nil {
		return nil, fmt.Errorf("get sonar compute engine task: %s info error: %v", taskID, err)
	}
	return res, nil
}

func (c *Client) GetComponentMeasures(component, branch string) (*MeasuresComponentResponse, error) {
	resp := &MeasuresComponentResponse{}
	rfs := []httpclient.RequestFunc{
		httpclient.SetQueryParam("component", component),
		httpclient.SetQueryParam("metricKeys", "ncloc,bugs,vulnerabilities,code_smells,coverage"), httpclient.SetResult(resp),
	}

	if branch != "" {
		rfs = append(rfs, httpclient.SetQueryParam("branch", branch))
	}

	url := "/api/measures/component"
	if _, err := c.Client.Get(url, rfs...); err != nil {
		return nil, fmt.Errorf("failed to search sonar component measures, component: %s, branch: %s, error: %v", component, branch, err)
	}
	return resp, nil
}

type QualityGateStatus string

const (
	QualityGateError QualityGateStatus = "ERROR"
	QualityGateOK    QualityGateStatus = "OK"
	QualityGateWarn  QualityGateStatus = "WARN"
	QualityGateNone  QualityGateStatus = "None"
)

type ProjectInfo struct {
	ProjectStatus ProjectStatus
}

type ProjectStatus struct {
	Status     QualityGateStatus `json:"status"`
	Conditions []Condition       `json:"conditions"`
}

type Condition struct {
	Status         QualityGateStatus `json:"status"`
	MetricKey      string            `json:"metricKey"`
	Comparator     string            `json:"comparator"`
	PeriodIndex    int               `json:"periodIndex"`
	ErrorThreshold string            `json:"errorThreshold"`
	ActualValue    string            `json:"actualValue"`
}

func (c *Client) GetQualityGateInfo(analysisID string) (*ProjectInfo, error) {
	url := "/api/qualitygates/project_status"
	res := &ProjectInfo{}
	if _, err := c.Client.Get(url,
		httpclient.SetQueryParam("analysisId", analysisID),
		httpclient.SetResult(res),
	); err != nil {
		return nil, fmt.Errorf("get sonar quality gate: %s info error: %v", analysisID, err)
	}
	return res, nil
}

func (c *Client) WaitForCETaskTobeDone(taskID string, timeout time.Duration) (string, error) {
	timeouts := time.After(timeout)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-timeouts:
			return "", errors.New("sonar ce task excution timeout 10m")
		case <-ticker.C:
			taskInfo, err := c.GetCETaskInfo(taskID)
			if err != nil {
				return "", fmt.Errorf("get sonar ce task info error: %v", err)
			}
			if taskInfo.Task.Status == CETaskSuccess {
				return taskInfo.Task.AnalysisID, nil
			}
			if taskInfo.Task.Status == CETaskCanceled || taskInfo.Task.Status == CETaskFailed {
				return "", fmt.Errorf("sonar ce task status was %s", taskInfo.Task.Status)
			}
		}
	}
}

func GetSonarWorkDir(content string) string {
	return getKeyValue(content, SonarWorkDirKey)
}

func GetSonarCETaskID(content string) string {
	return getKeyValue(content, CETaskIDKey)
}

func GetProjectKey(content string) string {
	return getKeyValue(content, ProjectKey)
}

func GetBranch(content string) string {
	return getKeyValue(content, BranchKey)
}

func getKeyValue(content, inputKey string) string {
	kvStrs := strings.Split(content, "\n")
	for _, kvStr := range kvStrs {
		kvStr = strings.TrimSpace(string(kvStr))
		index := strings.Index(kvStr, "=")
		if index < 0 {
			continue
		}
		key := strings.TrimSpace(kvStr[:index])
		if len(key) == 0 {
			continue
		}
		if key != inputKey {
			continue
		}
		return strings.TrimSpace(kvStr[index+1:])
	}
	return ""
}

func PrintSonarConditionTables(conditions []Condition) {
	log.Infof("")
	log.Infof("%-40s|%-10s|%-10s|%-10s|%-20s|", "Metric", "Status", "Operator", "Threshold", "Actualvalue")
	for _, condition := range conditions {
		log.Infof("%-40s|%-10s|%-10s|%-10s|%-20s|", condition.MetricKey, condition.Status, condition.Comparator, condition.ErrorThreshold, condition.ActualValue)
	}
	log.Infof("")
}

func VMPrintSonarConditionTables(conditions []Condition, logger *vmlog.JobLogger) {
	logger.Infof("")
	logger.Infof("%-40s|%-10s|%-10s|%-10s|%-20s|", "Metric", "Status", "Operator", "Threshold", "Actualvalue")
	for _, condition := range conditions {
		logger.Infof("%-40s|%-10s|%-10s|%-10s|%-20s|", condition.MetricKey, condition.Status, condition.Comparator, condition.ErrorThreshold, condition.ActualValue)
	}
	logger.Infof("")
}

func GetSonarProjectKeyFromConfig(config string) string {
	v := viper.New()
	v.SetConfigType("properties")
	err := v.ReadConfig(strings.NewReader(config))
	if err != nil {
		return ""
	}
	// Returns the empty string if sonar.projectKey is empty or does not exist.
	key, _ := v.Get("sonar.projectKey").(string)
	return key
}

func GetSonarBranchFromConfig(config string) string {
	v := viper.New()
	v.SetConfigType("properties")
	err := v.ReadConfig(strings.NewReader(config))
	if err != nil {
		return ""
	}
	// Returns the empty string if sonar.branch.name is empty or does not exist.
	key, _ := v.Get("sonar.branch.name").(string)
	return key
}

// GetSonarAddress return the corresponding project address according to projectKey
// If the projectKey is empty or an error occurs, the original baseAddr is returned
func GetSonarAddress(baseAddr, projectKey, branch string) (string, error) {
	if projectKey == "" {
		return baseAddr, nil
	}
	u, err := url.Parse(baseAddr)
	if err != nil {
		return baseAddr, fmt.Errorf("failed to parse sonar server address, error: %s", err)
	}
	u = u.JoinPath("dashboard")

	values := url.Values{"id": {projectKey}}
	if branch != "" {
		values["branch"] = []string{branch}
	}

	u.RawQuery = values.Encode()
	return u.String(), nil
}
