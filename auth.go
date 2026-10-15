package main

import (
	"context"
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
	"github.com/oldblocker/lantern-headless-iran/auth"
	"github.com/pterm/pterm"
)

type authData struct {
	UserID    int64
	UserToken string
}

func readAuth() {
	var data authData
	if _, err := toml.DecodeFile(configFilePath, &data); err != nil {
		pterm.Debug.Printfln("Error reading config. Using defaults. Error: %s", err.Error())
		userToken = ""
		userID = 0
	} else {
		pterm.Debug.Printfln("Read config: %v", data)
		userID = data.UserID
		userToken = data.UserToken
	}
}

func login(ctx context.Context, client auth.Client, email, password string) error {
	sp, _ := spinner.Start("Logging in")
	resp, _, err := client.Login(ctx, email, password, deviceId)
	sp.Stop()
	if err != nil {
		return fmt.Errorf("error logging in: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("error logging in: server rejected the login")
	}

	userID = resp.LegacyID
	userToken = resp.LegacyToken

	pterm.Success.Println("Logged in successfully")
	return persistAuth()
}

func persistAuth() error {
	f, err := os.Create(configFilePath)
	if err != nil {
		return fmt.Errorf("error creating config file: %w", err)
	}
	defer func(f *os.File) {
		_ = f.Close()
	}(f)
	if err = toml.NewEncoder(f).Encode(authData{
		userID,
		userToken,
	}); err != nil {
		return fmt.Errorf("error writing config: %w", err)
	}
	return nil
}

var passwordInput = pterm.DefaultInteractiveTextInput.WithMask("*")

func askForCredentials(inputEmail, inputPassword string) (string, string) {
	email, password := inputEmail, inputPassword
	if inputEmail == "" {
		email, _ = pterm.DefaultInteractiveTextInput.Show("Enter your email")
	}
	if inputPassword == "" {
		password, _ = passwordInput.Show("Enter your password")
	}
	if email == "" || password == "" {
		pterm.Error.Println("Email and password are required")
		os.Exit(2)
	}
	return email, password
}

func signup(ctx context.Context, client auth.Client, email, password string) error {
	sp, _ := spinner.Start("Signing up")
	_, err := client.SignUp(ctx, email, password)
	sp.Stop()
	if err != nil {
		return fmt.Errorf("error signing up: %w", err)
	}

	pterm.Info.Printfln("A confirmation code was sent to %s", email)
	code, _ := pterm.DefaultInteractiveTextInput.Show("Enter the confirmation code from your email")
	if code == "" {
		return fmt.Errorf("confirmation code is required")
	}

	result, err := client.SignupEmailConfirmation(ctx, &auth.ConfirmSignupRequest{
		Email: email,
		Code:  code,
	})
	if err != nil {
		return fmt.Errorf("error confirming email: %w", err)
	}
	if !result {
		return fmt.Errorf("error confirming email: server rejected the code")
	}

	pterm.Info.Println("Email confirmed successfully, logging in")
	return login(ctx, client, email, password)
}

func logout() {
	userID = 0
	userToken = ""
	_ = persistAuth()
}

func authClientOptions() []auth.Option {
	var o []auth.Option
	if args.UpstreamProxy != "" {
		o = append(o, auth.WithUpstreamProxy(args.UpstreamProxy))
	}
	return o
}

func authCmd(ctx context.Context, cmd *AuthCmd, overrideAuthURL string) {
	authUrl := auth.DefaultAPIURL
	if overrideAuthURL != "" {
		authUrl = overrideAuthURL
	}
	client := auth.NewClient(authUrl, args.Insecure, authClientOptions()...)
	var err error
	switch {
	case cmd.Login != nil:
		u, p := askForCredentials(cmd.Login.Email, cmd.Login.Password)
		err = login(ctx, client, u, p)
	case cmd.Signup != nil:
		u, p := askForCredentials(cmd.Signup.Email, cmd.Signup.Password)
		err = signup(ctx, client, u, p)
	case cmd.Logout != nil:
		logout()
	default:
		printHelp()
		os.Exit(2)
	}

	if err != nil {
		pterm.Error.Printfln("Error authenticating: %s", err.Error())
		os.Exit(2)
	}
}
