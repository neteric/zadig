package step

import (
	"bufio"
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"gopkg.in/yaml.v2"

	"github.com/koderover/zadig/v2/pkg/microservice/reaper/config"
	"github.com/koderover/zadig/v2/pkg/setting"
	"github.com/koderover/zadig/v2/pkg/tool/httpclient"
	"github.com/koderover/zadig/v2/pkg/tool/log"
	s3tool "github.com/koderover/zadig/v2/pkg/tool/s3"
	"github.com/koderover/zadig/v2/pkg/types/step"
)

type ToolInstallStep struct {
	spec       *step.StepToolInstallSpec
	envs       []string
	secretEnvs []string
	workspace  string
}

func NewToolInstallStep(spec interface{}, workspace string, envs, secretEnvs []string) (*ToolInstallStep, error) {
	toolInstallStep := &ToolInstallStep{workspace: workspace, envs: envs, secretEnvs: secretEnvs}
	yamlBytes, err := yaml.Marshal(spec)
	if err != nil {
		return toolInstallStep, fmt.Errorf("marshal spec %+v failed", spec)
	}
	if err := yaml.Unmarshal(yamlBytes, &toolInstallStep.spec); err != nil {
		return toolInstallStep, fmt.Errorf("unmarshal spec %s to tool install spec failed", yamlBytes)
	}
	return toolInstallStep, nil
}

func (s *ToolInstallStep) Run(ctx context.Context) error {
	start := time.Now()
	log.Infof("Installing tools.")
	defer func() {
		log.Infof("Install tools ended. Duration: %.2f seconds.", time.Since(start).Seconds())
	}()

	for _, tool := range s.spec.Installs {
		log.Infof("Installing %s %s.", tool.Name, tool.Version)
		if err := s.runIntallationScripts(tool); err != nil {
			return err
		}
	}

	return nil
}

func (s *ToolInstallStep) runIntallationScripts(tool *step.Tool) error {
	if tool == nil {
		return nil
	}
	var (
		openProxy                   bool
		proxyScript, disProxyScript string
	)

	var tmpPath string
	scripts := []string{}
	scripts = append(scripts, "set -ex")

	// 获取用户指定环境变量
	s.envs = append(s.envs, Environs(tool.Envs)...)

	if openProxy {
		scripts = append(scripts, proxyScript)
	}

	// 如果应用有配置下载路径
	if tool.Download != "" {
		s.spec.S3Storage.Subfolder = fmt.Sprintf("%s/%s-v%s", config.ConstructCachePath, tool.Name, tool.Version)
		filepath := strings.Split(tool.Download, "/")
		fileName := filepath[len(filepath)-1]

		tmpPath = path.Join(os.TempDir(), fileName)
		s3client, err := s3tool.NewClient(s.spec.S3Storage.Endpoint, s.spec.S3Storage.Ak, s.spec.S3Storage.Sk, s.spec.S3Storage.Region, s.spec.S3Storage.Insecure, s.spec.S3Storage.Provider)
		if err == nil {
			objectKey := GetObjectPath(fileName, s.spec.S3Storage.Subfolder)
			err = s3client.Download(
				s.spec.S3Storage.Bucket,
				objectKey,
				tmpPath,
			)

			// 缓存不存在
			if err != nil {
				err := httpclient.Download(tool.Download, tmpPath)
				if err != nil {
					return fmt.Errorf("download package %s error: %v", tool.Download, err)
				}
				s3client.Upload(
					s.spec.S3Storage.Bucket,
					tmpPath,
					objectKey,
				)
				log.Infof("Package loaded from url: %s", tool.Download)
			}
		} else {
			err := httpclient.Download(tool.Download, tmpPath)
			if err != nil {
				return err
			}
		}
	}

	for j, command := range tool.Scripts {
		realCommand := strings.ReplaceAll(command, config.FilepathParam, tmpPath)
		tool.Scripts[j] = realCommand
	}

	scripts = append(scripts, tool.Scripts...)

	if openProxy {
		scripts = append(scripts, disProxyScript)
	}
	uid, _ := uuid.NewUUID()
	file := filepath.Join(os.TempDir(), fmt.Sprintf("install_script_%d.sh", uid.ID()))
	if err := ioutil.WriteFile(file, []byte(strings.Join(scripts, "\n")), 0700); err != nil {
		return fmt.Errorf("write script file error: %v", err)
	}

	cmd := exec.Command("/bin/bash", file)

	cmdOutReader, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	outScanner := bufio.NewScanner(cmdOutReader)
	go func() {
		for outScanner.Scan() {
			fmt.Printf("%s   %s\n", time.Now().Format(setting.WorkflowTimeFormat), outScanner.Text())
		}
	}()

	cmdErrReader, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	errScanner := bufio.NewScanner(cmdErrReader)
	go func() {
		for errScanner.Scan() {
			fmt.Printf("%s   %s\n", time.Now().Format(setting.WorkflowTimeFormat), errScanner.Text())
		}
	}()

	cmd.Dir = s.workspace
	cmd.Env = s.envs

	if err := cmd.Run(); err != nil {
		return err
	}

	return nil
}

func Environs(envs []string) []string {
	resp := []string{}
	for _, val := range envs {
		if val == "" {
			continue
		}

		if len(strings.Split(val, "=")) != 2 {
			continue
		}

		replaced := strings.Replace(val, "$HOME", config.Home(), -1)
		resp = append(resp, replaced)
	}
	return resp
}

func GetObjectPath(name, subFolder string) string {
	// target should not be started with /
	if subFolder != "" {
		return strings.TrimLeft(filepath.Join(subFolder, name), "/")
	}

	return strings.TrimLeft(name, "/")
}
