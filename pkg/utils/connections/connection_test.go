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

package connections

import (
	"bytes"
	"encoding/json"
	"flag"
	"io"
	"io/ioutil"
	"net/http"
	"testing"

	"github.com/eclipse/codewind-installer/pkg/apiroutes"
	"github.com/stretchr/testify/assert"
	"github.com/urfave/cli"
)

type ClientMockServerConfig struct {
	StatusCode int
	Body       io.ReadCloser
}

func (c *ClientMockServerConfig) Do(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: c.StatusCode,
		Body:       c.Body,
	}, nil
}

// Test_SchemaUpgrade01 :  Upgrade schema tests from Version 0 to Version 1
func Test_SchemaUpgrade0to1(t *testing.T) {
	// create a v1 file :
	v1File := "{\"connections\": [{\"name\":\"testlocal\",\"label\": \"Codewind local test connection\",\"url\": \"\"}]}"
	ioutil.WriteFile(GetConnectionConfigFilename(), []byte(v1File), 0644)
	t.Run("Asserts schema updated to v1 with a local target", func(t *testing.T) {
		InitConfigFileIfRequired() // perform upgrade
		result, err := GetConnectionsConfig()
		if err != nil {
			t.Fail()
		}
		assert.Equal(t, 1, result.SchemaVersion)
		assert.Len(t, result.Connections, 1)
		assert.Equal(t, "testlocal", result.Connections[0].ID)
	})
}

func Test_GetConnectionsConfig(t *testing.T) {
	t.Run("Asserts there is only one connection", func(t *testing.T) {
		ResetConnectionsFile()
		result, err := GetConnectionsConfig()
		if err != nil {
			t.Fail()
		}
		assert.Len(t, result.Connections, 1)
	})
}

// Test_CreateNewConnection :  Adds a new connection to the list called remoteserver
func Test_CreateNewConnection(t *testing.T) {
	set := flag.NewFlagSet("tests", 0)
	set.String("label", "MyRemoteServer", "just a label")
	set.String("url", "https://codewind.server.remote", "Codewind URL")
	c := cli.NewContext(nil, set, nil)

	ResetConnectionsFile()

	mockResponse := apiroutes.GatekeeperEnvironment{AuthURL: "http://a.mock.auth.server.remote:1234", Realm: "remoteRealm", ClientID: "remoteClient"}
	jsonResponse, _ := json.Marshal(mockResponse)
	body := ioutil.NopCloser(bytes.NewReader([]byte(jsonResponse)))

	// construct a http client with our mock canned response
	mockClient := &ClientMockServerConfig{StatusCode: http.StatusOK, Body: body}

	t.Run("Adds new connection to the config", func(t *testing.T) {
		AddConnectionToList(mockClient, c)
		result, err := GetConnectionsConfig()
		if err != nil {
			t.Fail()
		}
		assert.Len(t, result.Connections, 2)
	})
}

// Test_RemoveConnectionFromList : Adds a new connection to the stored list
func Test_RemoveConnectionFromList(t *testing.T) {
	set := flag.NewFlagSet("tests", 0)

	allConnections, err := GetAllConnections()
	if err != nil {
		t.Fail()
	}

	idToDelete := allConnections[1].ID

	set.String("conid", idToDelete, "doc")
	c := cli.NewContext(nil, set, nil)

	t.Run("Check we have 2 connections", func(t *testing.T) {
		result, err := GetConnectionsConfig()
		if err != nil {
			t.Fail()
		}
		assert.Len(t, result.Connections, 2)
	})

	t.Run("Remove the https://codewind.server.remote connection", func(t *testing.T) {
		RemoveConnectionFromList(c)
		result, err := GetConnectionsConfig()
		if err != nil {
			t.Fail()
		}
		assert.Len(t, result.Connections, 1)
	})
}
