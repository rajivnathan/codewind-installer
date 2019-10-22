/*******************************************************************************
 * Copyright (c) 2019 IBM Corporation and others.
 * All rights reserved. This program and the accompanying materials
 * are made available under the terms of the Eclipse Public License v2.0
 * which accompanies this distribution, and is available at
 * http://www.eclipse.org/legal/epl-v20.html
 *
 * Contributors:
 *     IBM Corporation - initial API and implementation
 *******************************************************************************/

package actions

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/eclipse/codewind-installer/apiroutes"
	"github.com/eclipse/codewind-installer/config"
	"github.com/eclipse/codewind-installer/errors"
	"github.com/eclipse/codewind-installer/utils"
	"github.com/urfave/cli"
)

type (
	// ProjectType represents the information Codewind requires to build a project.
	ProjectType struct {
		Language  string `json:"language"`
		BuildType string `json:"projectType"`
	}

	// ValidationResponse represents the response to validating a project on the users filesystem
	// result is an interface as it could be ProjectType or string depending on success or failure.
	BindRequest struct {
		Language    string `json:"language"`
		ProjectType string `json:"projectType"`
		Name        string `json:"name"`
		Path        string `json:"path"`
	}

	BindEndRequest struct {
		ProjectID string `json:"id"`
	}

	CompleteRequest struct {
		FileList     []string `json:"fileList"`
		ModifiedList []string `json:"modifiedList"`
		TimeStamp    int64    `json:"timeStamp"`
	}

	FileUploadMsg struct {
		IsDirectory  bool   `json:"isDirectory"`
		RelativePath string `json:"path"`
		Message      string `json:"msg"`
	}

	// ValidationResponse represents the response to validating a project on the users filesystem.
	ValidationResponse struct {
		Status string      `json:"status"`
		Path   string      `json:"projectPath"`
		Result interface{} `json:"result"`
	}
)

// DownloadTemplate using the url/link provided
func DownloadTemplate(c *cli.Context) {
	destination := c.Args().Get(0)

	if destination == "" {
		log.Fatal("destination not set")
	}

	projectDir := path.Base(destination)

	// Remove invalid characters from the string we will use
	// as the project name in the template.
	r := regexp.MustCompile("[^a-zA-Z0-9._-]")
	projectName := r.ReplaceAllString(projectDir, "")
	if len(projectName) == 0 {
		projectName = "PROJ_NAME_PLACEHOLDER"
	}

	url := c.String("u")

	err := utils.DownloadFromURLThenExtract(url, destination)
	if err != nil {
		log.Fatal(err)
	}
	err = utils.ReplaceInFiles(destination, "[PROJ_NAME_PLACEHOLDER]", projectName)
	if err != nil {
		log.Fatal(err)
	}
}

// checkIsExtension checks if a project is an extension project and run associated commands as necessary
func checkIsExtension(projectPath string, c *cli.Context) (string, error) {

	extensions, err := apiroutes.GetExtensions()
	if err != nil {
		log.Println("There was a problem retrieving extensions data")
		return "unknown", err
	}

	params := make(map[string]string)
	commandName := "postProjectValidate"

	// determine if type:subtype hint was given
	// but only if url was not given
	if c.String("u") == "" && c.String("t") != "" {
		parts := strings.Split(c.String("t"), ":")
		params["type"] = parts[0]
		if len(parts) > 1 {
			params["subtype"] = parts[1]
		}
		commandName = "postProjectValidateWithType"
	}

	for _, extension := range extensions {

		var isMatch bool

		if len(params) > 0 {
			// check if extension project type matched the hinted type
			isMatch = extension.ProjectType == params["type"]
		} else {
			// check if project contains the detection file an extension defines
			isMatch = extension.Detection != "" && utils.PathExists(path.Join(projectPath, extension.Detection))
		}

		if isMatch {

			var cmdErr error

			// check if there are any commands to run
			for _, command := range extension.Commands {
				if command.Name == commandName {
					cmdErr = utils.RunCommand(projectPath, command, params)
					break
				}
			}

			return extension.ProjectType, cmdErr
		}
	}

	return "", nil
}

// ValidateProject returns the language and buildType for a project at given filesystem path,
// and writes a default .cw-settings file to that project
func ValidateProject(c *cli.Context) {
	projectPath := c.Args().Get(0)
	utils.CheckProjectPath(projectPath)
	validationStatus := "success"
	// result could be ProjectType or string, so define as an interface
	var validationResult interface{}
	language, buildType := utils.DetermineProjectInfo(projectPath)
	validationResult = ProjectType{
		Language:  language,
		BuildType: buildType,
	}
	extensionType, err := checkIsExtension(projectPath, c)
	if extensionType != "" {
		if err == nil {
			validationResult = ProjectType{
				Language:  language,
				BuildType: extensionType,
			}
		} else {
			validationStatus = "failed"
			validationResult = err.Error()
		}
	}

	response := ValidationResponse{
		Status: validationStatus,
		Path:   projectPath,
		Result: validationResult,
	}
	projectInfo, err := json.Marshal(response)

	errors.CheckErr(err, 203, "")
	// write settings file only for non-extension projects
	if extensionType == "" {
		writeCwSettingsIfNotInProject(projectPath, buildType)
	}
	fmt.Println(string(projectInfo))
}

func BindProject(c *cli.Context) {
	projectPath := strings.TrimSpace(c.String("path"))
	Name := strings.TrimSpace(c.String("name"))
	Language := strings.TrimSpace(c.String("language"))
	BuildType := strings.TrimSpace(c.String("type"))

	bindRequest := BindRequest{
		Language:    Language,
		Name:        Name,
		ProjectType: BuildType,
		Path:        projectPath,
	}
	buf := new(bytes.Buffer)
	json.NewEncoder(buf).Encode(bindRequest)

	//	fmt.Println(buf)
	// Make the request to start the remote bind process.
	remotebindUrl := config.PFEApiRoute() + "projects/remote-bind/start"
	//	fmt.Println("Posting to: " + remotebindUrl)

	client := &http.Client{}

	request, err := http.NewRequest("POST", remotebindUrl, bytes.NewReader(buf.Bytes()))
	request.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(request)
	if err != nil {
		return
	}

	defer resp.Body.Close()
	bodyBytes, err := ioutil.ReadAll(resp.Body)
	body := string(bodyBytes)
	fmt.Println(string(body))

	var projectInfo map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &projectInfo); err != nil {
		panic(err)
		// TODO - Need to handle this gracefully.
	}

	projectID := projectInfo["projectID"].(string)
	fmt.Println("Returned projectid " + projectID)

	// Sync all the project files
	syncFiles(projectPath, projectID, 0)

	// Call remote-bind/end to complete
	completeRemotebind(projectID)
}

func completeRemotebind(projectId string) {
	uploadEndUrl := config.PFEApiRoute() + "projects/" + projectId + "/remote-bind/end"

	payload := &BindEndRequest{ProjectID: projectId}
	jsonPayload, _ := json.Marshal(payload)

	// Make the request to end the sync process.
	resp, err := http.Post(uploadEndUrl, "application/json", bytes.NewBuffer(jsonPayload))
	fmt.Println("Upload end status:" + resp.Status)
	if err != nil {
		panic(err)
		// TODO - Need to handle this gracefully.
	}

}

func syncFiles(projectPath string, projectId string, synctime int64) ([]string, []string) {
	var fileList []string
	var modifiedList []string

	projectUploadUrl := config.PFEApiRoute() + "projects/" + projectId + "/remote-bind/upload"
	client := &http.Client{}
	fmt.Println("Uploading to " + projectUploadUrl)

	err := filepath.Walk(projectPath, func(path string, info os.FileInfo, err error) error {

		if err != nil {
			// TODO - How to handle *some* files being unreadable
		}
		if !info.IsDir() {
			relativePath := path[(len(projectPath) + 1):]
			// Create list of all files for a project
			fileList = append(fileList, relativePath)

			// get time file was modified in milliseconds since epoch
			modifiedmillis := info.ModTime().UnixNano() / 1000000

			fileUploadBody := FileUploadMsg{
				IsDirectory:  info.IsDir(),
				RelativePath: relativePath,
				Message:      "",
			}

			// Has this file been modified since last sync
			if modifiedmillis > synctime {
				fileContent, err := ioutil.ReadFile(path)
				jsonContent, err := json.Marshal(string(fileContent))
				// Skip this file if there is an error reading it.
				if err != nil {
					return nil
				}
				// Create list of all modfied files
				modifiedList = append(modifiedList, relativePath)

				var buffer bytes.Buffer
				zWriter := zlib.NewWriter(&buffer)
				zWriter.Write([]byte(jsonContent))

				zWriter.Close()
				encoded := base64.StdEncoding.EncodeToString(buffer.Bytes())
				fileUploadBody.Message = encoded

				buf := new(bytes.Buffer)
				json.NewEncoder(buf).Encode(fileUploadBody)

				// TODO - How do we handle partial success?
				request, err := http.NewRequest("PUT", projectUploadUrl, bytes.NewReader(buf.Bytes()))
				request.Header.Set("Content-Type", "application/json")
				resp, err := client.Do(request)
				fmt.Println("Upload status:" + resp.Status + " for file: " + relativePath)
				if err != nil {
					return nil
				}
			}
		}

		return nil
	})
	if err != nil {
		fmt.Printf("error walking the path %q: %v\n", projectPath, err)
		return nil, nil
	}
	return fileList, modifiedList
}

func SyncProject(c *cli.Context) {
	projectPath := strings.TrimSpace(c.String("path"))
	projectID := strings.TrimSpace(c.String("id"))
	synctime := int64(c.Int("time"))

	// Sync all the necessary project files
	fileList, modifiedList := syncFiles(projectPath, projectID, synctime)
	fmt.Println(fileList)
	fmt.Println(modifiedList)

	// Complete the upload
	completeUpload(projectID, fileList, modifiedList, synctime)
}

func completeUpload(projectId string, files []string, modfiles []string, timestamp int64) {
	uploadEndUrl := config.PFEApiRoute() + "projects/" + projectId + "/upload/end"

	payload := &CompleteRequest{FileList: files, ModifiedList: modfiles, TimeStamp: timestamp}
	jsonPayload, _ := json.Marshal(payload)

	// Make the request to end the sync process.
	resp, err := http.Post(uploadEndUrl, "application/json", bytes.NewBuffer(jsonPayload))
	fmt.Println("Upload end status:" + resp.Status)
	if err != nil {
		panic(err)
		// TODO - Need to handle this gracefully.
	}
}

func writeCwSettingsIfNotInProject(projectPath string, BuildType string) {
	pathToCwSettings := path.Join(projectPath, ".cw-settings")
	pathToLegacySettings := path.Join(projectPath, ".mc-settings")

	if _, err := os.Stat(pathToLegacySettings); os.IsExist(err) {
		utils.RenameLegacySettings(pathToLegacySettings, pathToCwSettings)
	} else if _, err := os.Stat(pathToCwSettings); os.IsNotExist(err) {
		utils.WriteNewCwSettings(pathToCwSettings, BuildType)
	}
}
