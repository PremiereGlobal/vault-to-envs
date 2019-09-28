package vaulttoenvs

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/sts"
	VaultApi "github.com/hashicorp/vault/api"
)

// Logger is a log interface for passing custom loggers
type Logger interface {
	Debug(args ...interface{})
	Info(args ...interface{})
	Warn(args ...interface{})
	Fatal(args ...interface{})
}

// SecretItem holds data about a secret config
type SecretItem struct {
	SecretPath         string            `json:"vault_path" yaml:"secretPath"`
	TTL                int               `json:"ttl" yaml:"ttl"`
	Version            float64           `json:"version" yaml:"version"`
	SecretMaps         map[string]string `json:"set" yaml:"set"`
	secretDataPath     string            // kv v2
	secretMetadataPath string            // kv v2
	effectiveVersion   int               // kv v2
	secretMapValues    map[string]string
	secret             *VaultApi.Secret
	mount              *VaultApi.MountOutput
}

// Config contains the vault-to-env configuration
type Config struct {
	VaultAddr        string
	vaultToken       string
	Debug            bool
	SecretConfig     string
	SecretConfigFile string
}

// VaultToEnvs is the main struct for this package
type VaultToEnvs struct {
	config           *Config
	vaultClient      *VaultApi.Client
	log              log
	secretMountTypes map[string]*VaultApi.MountOutput
	secretItems      []*SecretItem
}

// NewVaultToEnvs creates a new VaultToEnvs
func NewVaultToEnvs(config *Config) *VaultToEnvs {
	v2e := VaultToEnvs{
		config: config,
		log:    log{},
	}
	return &v2e
}

// SetLogger allows a custom logger to be used for messages
func (v *VaultToEnvs) SetLogger(logger Logger) {
	v.log.logger = logger
}

// SetVaultToken sets the Vault token
func (v *VaultToEnvs) SetVaultToken(token string) {
	v.config.vaultToken = token
}

func (v *VaultToEnvs) AddSecretItems(items ...*SecretItem) {
	v.secretItems = append(v.secretItems, items...)
}

func (v *VaultToEnvs) loadSecrets() error {

	var err error

	// Configure new Vault Client
	conf := &VaultApi.Config{Address: v.config.VaultAddr}
	v.vaultClient, err = VaultApi.NewClient(conf)
	if err != nil {
		return err
	}
	v.vaultClient.SetToken(v.config.vaultToken)

	// Pull together the mount types
	mountOutput, err := v.vaultClient.Sys().ListMounts()
	if err != nil {
		return fmt.Errorf("Error fetching mounts: %s", err.Error())
	}

	v.secretMountTypes = make(map[string]*VaultApi.MountOutput)
	for mountPath, mountData := range mountOutput {
		v.secretMountTypes[mountPath] = mountData
	}

	// Open/Parse secret config data
	var secretConfigData []byte
	if v.config.SecretConfigFile != "" {
		file, err := os.Open(v.config.SecretConfigFile)
		if err != nil {
			return fmt.Errorf("Error opening config file '%s': %v", v.config.SecretConfigFile, err)
		}
		defer file.Close()

		secretConfigData, err = ioutil.ReadAll(file)
		if err != nil {
			return fmt.Errorf("Error reading config file '%s': %v", v.config.SecretConfigFile, err)
		}
	} else if v.config.SecretConfig != "" {
		secretConfigData = []byte(v.config.SecretConfig)
	}

	if secretConfigData != nil {
		var secretItems []*SecretItem
		err = json.Unmarshal(secretConfigData, &secretItems)
		if err != nil {
			if terr, ok := err.(*json.UnmarshalTypeError); ok {
				return fmt.Errorf("Failed to parse secret config field %s: %v", terr.Field, terr)
			}

			return fmt.Errorf("Error parsing secret config: %v", err)
		}
		v.secretItems = append(v.secretItems, secretItems...)
	}

	// Retrieve the secrets from Vault
	for i, secretItem := range v.secretItems {

		if secretItem.SecretPath == "" {
			return fmt.Errorf("Error: secret_path not specified in secret config for item %d", i+1)
		}

		if len(secretItem.SecretMaps) < 1 {
			return fmt.Errorf("No env exports set for secret %s", secretItem.SecretPath)
		}

		secretItem.secretMapValues = make(map[string]string)
		pathParts := strings.Split(secretItem.SecretPath, "/")
		secretItem.mount = v.secretMountTypes[pathParts[0]+"/"]
		err := v.getSecret(secretItem)
		if err != nil {
			return err
		}
	}

	// Loop through secretItems and, if the mount has type aws, wait for AWS credentials to become active
	// TODO: Could probably do this in some sort of multithread manner
	for _, secretItem := range v.secretItems {
		if secretItem.mount.Type == "aws" {
			err := v.waitForAwsCredsToActivate(secretItem)
			if err != nil {
				return err
			}
		}
	}

	// TODO: Zero out the secret from memory
	// TODO: Revoke dynamic secrets on failure

	return nil
}

func (v *VaultToEnvs) getSecret(secretItem *SecretItem) error {

	var err error

	if secretItem.mount.Type == "kv" {
		err = v.GetKV2Secret(secretItem)
		if err != nil {
			return err
		}
	} else {

		// Ensure that non-v2 key-value stores don't have version set
		if secretItem.Version != 0 {
			return fmt.Errorf("Version specified on non-versioned secret: %s", secretItem.SecretPath)
		}

		// Add the 'data' subpath if it doesn't exist for v2 secret stores
		pathParts := strings.Split(secretItem.SecretPath, "/")
		if secretItem.mount.Type == "kv" && pathParts[1] != "data" {
			secretItem.SecretPath = path.Join(pathParts[0], "data", strings.Join(pathParts[1:], "/"))
		}

		// Read the secret from Vault
		var secret *VaultApi.Secret
		v.log.Info("Fetching secret: ", secretItem.SecretPath)
		secret, err = v.vaultClient.Logical().Read(secretItem.SecretPath)
		if err != nil {
			return fmt.Errorf("Error fetching secret: %s", err.Error())
		}

		// If we got back an empty response, fail
		if secret == nil {
			return fmt.Errorf("Could not find secret %s", secretItem.SecretPath)
		}

		secretItem.secret = secret

		for envName, secretKeyName := range secretItem.SecretMaps {
			if secret.Data[secretKeyName] == nil {
				return fmt.Errorf("Key %s not found in secret %s", secretKeyName, secretItem.SecretPath)
			}
			secretItem.secretMapValues[envName] = secret.Data[secretKeyName].(string)
		}
	}

	// Ensure that secret is renewable if trying to set the TTL
	if secretItem.TTL != 0 && !secretItem.secret.Renewable {
		return fmt.Errorf("Cannot set TTL on secret %s. TTL can only be set on dynamic secrets like AWS credentials", secretItem.SecretPath)
	} else if secretItem.TTL == 0 && secretItem.secret.Renewable {
		v.log.Info(fmt.Sprintf("Lease for %s: %s; Duration: %d ", secretItem.SecretPath, secretItem.secret.LeaseID, secretItem.secret.LeaseDuration))
	} else if secretItem.TTL != 0 {
		v.log.Info("Renewing lease on ", secretItem.SecretPath, " to ", secretItem.TTL, " seconds")
		v.log.Info("Original Lease Info ", secretItem.secret.LeaseID, ",", secretItem.secret.LeaseDuration)
		renewedSecret, err := v.vaultClient.Sys().Renew(secretItem.secret.LeaseID, secretItem.TTL)
		if err != nil {
			return fmt.Errorf("Error renewing secret (setting TTL): %s", err.Error())
		}
		v.log.Info("New Lease Info ", renewedSecret.LeaseID, ",", renewedSecret.LeaseDuration)

		// Check if lease duration was able to be set to desired amount
		// Added some tolerance for any request delay
		if (secretItem.TTL - renewedSecret.LeaseDuration) > 5 {
			return fmt.Errorf("Not able to set TTL to desired amount. Desired: %d; Actual: %d", secretItem.TTL, renewedSecret.LeaseDuration)
		}
	}

	return nil
}

// DisplayEnvExports outputs the results to stdout
func (v *VaultToEnvs) DisplayEnvExports() error {

	err := v.loadSecrets()
	if err != nil {
		return err
	}

	for _, secretItem := range v.secretItems {
		for envName, secretValue := range secretItem.secretMapValues {

			// Prints the env variable line to stdout
			// Single quotes value and escapes single quotes in secret with '"'"'
			fmt.Printf("export %s='%s'\n", envName, strings.Replace(secretValue, "'", "'\"'\"'", -1))
		}
	}

	return nil
}

// GetEnvs returns the secret environment variables as a slice
func (v *VaultToEnvs) GetEnvs() ([]string, error) {
	err := v.loadSecrets()
	if err != nil {
		return nil, err
	}

	result := []string{}

	for _, secretItem := range v.secretItems {
		for envName, secretValue := range secretItem.secretMapValues {

			// Single quotes value and escapes single quotes in secret with '"'"'
			result = append(result, fmt.Sprintf("%s=%s", envName, strings.Replace(secretValue, "'", "'\"'\"'", -1)))
		}
	}

	return result, nil
}

// GetKV2Secret gets a key-value (version 2) secret
// Uses the `version` option to select the desired version.  This can be negative to go back x versions or positive to indicate
// the actual secret version
func (v *VaultToEnvs) GetKV2Secret(secretItem *SecretItem) error {

	// Create the data and metadata paths for the secret
	pathParts := strings.Split(secretItem.SecretPath, "/")
	if pathParts[1] != "data" {
		secretItem.secretDataPath = path.Join(pathParts[0], "data", strings.Join(pathParts[1:], "/"))
	} else {
		secretItem.secretDataPath = secretItem.SecretPath
	}
	secretItem.secretMetadataPath = path.Join(pathParts[0], "metadata", strings.Join(pathParts[1:], "/"))

	// Determine the version to pull
	if secretItem.Version >= 0 {
		secretItem.effectiveVersion = int(secretItem.Version)
	} else {
		secret, err := v.vaultClient.Logical().Read(secretItem.secretMetadataPath)
		if err != nil {
			return fmt.Errorf("Error fetching secret: %s", err.Error())
		}
		if secret == nil {
			return fmt.Errorf("Could not get secret metadata %s: Secret does not exist", secretItem.secretMetadataPath)
		}

		versionResults := secret.Data["versions"].(map[string]interface{})

		// Store the keys (version) in slice so we can order it
		var keys []int
		for k := range versionResults {
			intKey, err := strconv.Atoi(k)
			if err != nil {
				return fmt.Errorf("Error converting version number: %s", err.Error())
			}
			keys = append(keys, intKey)
		}
		sort.Ints(keys)

		// Find the first available (non-deleted) version
		i := int(secretItem.Version)
		done := false
		for !done {

			// If the index is out of bounds, error and bug out
			if i < (-1*len(keys) + 1) {
				done = true
				return fmt.Errorf("Unabled to find desired version %v for secret %s", secretItem.Version, secretItem.SecretPath)
			}

			// Vault version number
			currentVersion := keys[len(keys)-1+i]
			v.log.Debug(fmt.Sprintf("Checking secret version %d as valid match for provided value '%d' for secret %s", currentVersion, int(secretItem.Version), secretItem.SecretPath))

			// Vault version data
			versionData := versionResults[strconv.Itoa(currentVersion)].(map[string]interface{})
			deleteTime := versionData["deletion_time"].(string)
			isDestroyed := versionData["destroyed"].(bool)

			// If the version we're looking at has been deleted or destroyed, move deeper
			if deleteTime != "" || isDestroyed {
				i = i - 1
				v.log.Warn(fmt.Sprintf("Version %d of secret %s has been deleted, checking next version...", currentVersion, secretItem.SecretPath))
			} else {
				secretItem.effectiveVersion = currentVersion
				done = true
			}
		}
	}

	// Read the secret from Vault
	secretData := make(map[string][]string)
	secretData["version"] = []string{strconv.Itoa(secretItem.effectiveVersion)}
	v.log.Info(fmt.Sprintf("Fetching secret %s: version %d", secretItem.SecretPath, secretItem.effectiveVersion))
	secret, err := v.vaultClient.Logical().ReadWithData(secretItem.secretDataPath, secretData)
	if err != nil {
		return fmt.Errorf("Error fetching secret: %s", err.Error())
	}

	// If we got back an empty response, fail
	if secret == nil {
		return fmt.Errorf("Could not find secret %s: version %v", secretItem.SecretPath, secretItem.Version)
	}

	secretItem.secret = secret

	// Map the keys to the env values
	for envName, secretKeyName := range secretItem.SecretMaps {
		if secret.Data["data"] == nil {
			return fmt.Errorf("No data found in secret %s", secretItem.SecretPath)
		}

		data := secret.Data["data"].(map[string]interface{})

		if data[secretKeyName] == nil {
			return fmt.Errorf("Key %s not found in secret %s", secretKeyName, secretItem.SecretPath)
		}

		secretItem.secretMapValues[envName] = data[secretKeyName].(string)
	}

	return nil
}

func (v *VaultToEnvs) waitForAwsCredsToActivate(secretItem *SecretItem) error {

	// Retrieve ID/Key from secretItem
	var accessKey string
	var secretKey string
	for k, v := range secretItem.SecretMaps {
		if v == "access_key" {
			accessKey = secretItem.secretMapValues[k]
		} else if v == "secret_key" {
			secretKey = secretItem.secretMapValues[k]
		}
	}

	// Ensure both are set (if not the user didn't set them and we should error out)
	// TODO: make this happen before requesting the credentials
	if accessKey == "" {
		return fmt.Errorf("Vault key 'access_key' for AWS credential provider %s not assigned to ENV var", secretItem.SecretPath)
	}
	if secretKey == "" {
		return fmt.Errorf("Vault key 'secret_key' for AWS credential provider %s not assigned to ENV var", secretItem.SecretPath)
	}

	awsCreds := credentials.NewStaticCredentials(accessKey, secretKey, "")
	sess, err := session.NewSession(&aws.Config{
		Credentials: awsCreds},
	)
	if err != nil {
		return fmt.Errorf("Error creating AWS session: %s", err.Error())
	}

	// Create a IAM service client.
	svc := sts.New(sess)

	// Try to get caller identity until it becomes active
	err = retry(20, time.Second, func() error {

		_, err := svc.GetCallerIdentity(&sts.GetCallerIdentityInput{})
		if awserr, ok := err.(awserr.Error); ok {
			if awserr.Code() == "InvalidClientTokenId" {
				v.log.Info("AWS credentials not yet active, waiting...")
				return err
			}

			return fmt.Errorf("Error validating AWS credentials: %s", err.Error())
		}

		v.log.Info("AWS credentials (", accessKey, ") from ", secretItem.SecretPath, " active")
		return nil
	})

	if err != nil {
		return fmt.Errorf("Error validating AWS credentials (not active within set duration) %s", err.Error())
	}

	return nil

}

func retry(attempts int, sleep time.Duration, fn func() error) error {
	if err := fn(); err != nil {
		if s, ok := err.(stop); ok {
			// Return the original error for later checking
			return s.error
		}

		if attempts--; attempts > 0 {
			time.Sleep(sleep)
			return retry(attempts, 2*sleep, fn)
		}
		return err
	}
	return nil
}

type stop struct {
	error
}

type log struct {
	logger Logger
}

func (l *log) Debug(args ...interface{}) {
	if l.logger != nil {
		l.logger.Debug(args...)
	}
}

func (l *log) Info(args ...interface{}) {
	if l.logger != nil {
		l.logger.Info(args...)
	}
}

func (l *log) Warn(args ...interface{}) {
	if l.logger != nil {
		l.logger.Warn(args...)
	}
}
