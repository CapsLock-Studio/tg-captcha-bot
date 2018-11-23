package main

import (
	"os"
	"testing"
)

func TestReadConfig(t *testing.T) {
	err := readConfig()
	if err != nil {
		t.Errorf("Cannot read config file. Error: %v", err)
	}
}

func TestCorrectToken(t *testing.T) {
	token := "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11"
	os.Setenv("TGTOKEN", token)

	v, err := getToken()
	if err != nil {
		t.Errorf("Incorrect token. Error: %v", err)
	}

	if v != token {
		t.Errorf("Incorrect token. Expected: %v, Have: %v", token, v)
	}
}

func TestIncorrectToken(t *testing.T) {
	token := "a123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11"
	os.Setenv("TGTOKEN", token)

	v, _ := getToken()

	if v != "" {
		t.Errorf(`Case failed. Expected "", Have: %v`, v)
	}
}
