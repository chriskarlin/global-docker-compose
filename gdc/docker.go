package gdc

import (
	_ "embed" // to allow embedding the docker-compose file
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	"github.com/codeskyblue/go-sh"
)

//go:embed docker-compose.yml
var dcFile []byte
var outputFile = "./docker-compose-out.yml"

// NewComposeInfo with the given additional file and requested services
func NewComposeInfo(additionalFile string, requestedServices string) ComposeInfo {
	serviceArray := []string{}
	if len(requestedServices) > 0 {
		serviceArray = strings.Split(requestedServices, ",")
	}
	return ComposeInfo{
		MainFile:          dcFile,
		AdditionalFile:    additionalFile,
		RequestedServices: serviceArray,
	}
}

// save the in-memory docker-compose.yml file to disk so we can pass it in
// trying to pass it into stdin causes issues when there is an additional file
func writeDcFile() {
	// check if file exists
	_, err := os.Stat(outputFile)
	if err == nil {
		return
	}

	ioutil.WriteFile(outputFile, dcFile, 0644)
}

// Cleanup the output files.
func Cleanup() {
	os.Remove(outputFile)
}

// Exit cleanly from the program.
func Exit(message string, args ...interface{}) {
	Cleanup()
	if len(message) > 0 {
		if len(args) > 0 {
			fmt.Printf(message, args...)
			fmt.Println()
		} else {
			fmt.Println(message)
		}
	}
	os.Exit(1)
}

func exitServiceNotFound(compose ComposeInfo, command string, service string) {
	services := strings.Join(compose.configuredServices(), ", ")
	str := `
Cannot execute command %s - %s is not a known service!
Known services: %s
`
	Exit(str, command, service, services)
}

func validateService(compose ComposeInfo, command string, service string) {
	if !compose.IsServiceConfigured((service)) {
		exitServiceNotFound(compose, command, service)
	}
}

func serviceString(compose ComposeInfo, command string) string {
	if len(compose.RequestedServices) == 0 {
		Exit("No services provided for command %s! Use the --services option.", command)
	}
	results := []string{}
	for _, service := range compose.RequestedServices {
		if !compose.IsServiceConfigured(service) {
			exitServiceNotFound(compose, command, service)
		}
		results = append(results, service)
		if service == "kafka" {
			results = append(results, "schema-registry", "control-center")
		}
		if service == "redis" {
			results = append(results, "redisinsight")
		}
		if service == "dynamodb" {
			results = append(results, "dynamodb-admin")
		}
		if service == "opensearch" {
			results = append(results, "opensearch-dashboards")
		}
	}
	return strings.Join(results, " ")
}

func mainCommand(compose ComposeInfo) string {
	cmd := fmt.Sprintf("docker compose -p global -f %s", outputFile)
	if len(compose.AdditionalFile) > 0 {
		cmd = fmt.Sprintf("%s -f %s", cmd, compose.AdditionalFile)
	}
	return cmd
}

func executeDockerCommand(compose ComposeInfo, service string, command string, inputFile string) {
	if len(inputFile) > 0 {
		RunCommands(
			fmt.Sprintf("cat %s", inputFile),
			fmt.Sprintf("%s exec -T %s %s", mainCommand(compose), service, command),
		)
	} else {
		RunCommand("%s exec %s %s", mainCommand(compose), service, command)

	}
}

func ecrLogin() {
	RunCommands("aws ecr get-login-password", "docker login --password-stdin -u AWS 421990735784.dkr.ecr.us-east-1.amazonaws.com")
}

func ghcrLogin() {
	session := sh.NewSession()
	session.ShowCMD = false

	// Try gh CLI
	username, _ := session.Command("gh", "api", "user", "-q", ".login").Output()
	token, _ := session.Command("gh", "auth", "token").Output()

	if len(username) > 0 && len(token) > 0 {
		fmt.Println("Logging into GitHub Container Registry using GitHub CLI credentials...")
		loginSession := sh.NewSession()
		loginSession.SetStdin(strings.NewReader(string(token)))
		err := loginSession.Command("docker", "login", "ghcr.io", "-u", strings.TrimSpace(string(username)), "--password-stdin").Run()
		if err == nil {
			fmt.Printf("Successfully logged into ghcr.io as %s\n", strings.TrimSpace(string(username)))
			return
		}
		fmt.Printf("Failed to login with GitHub CLI: %v\n", err)
	}

	// Try environment variables
	githubToken := os.Getenv("GITHUB_TOKEN")
	githubUsername := os.Getenv("GITHUB_USERNAME")
	if githubToken != "" && githubUsername != "" {
		fmt.Println("Logging into GitHub Container Registry using environment variables...")
		loginSession := sh.NewSession()
		loginSession.SetStdin(strings.NewReader(githubToken))
		err := loginSession.Command("docker", "login", "ghcr.io", "-u", githubUsername, "--password-stdin").Run()
		if err == nil {
			fmt.Printf("Successfully logged into ghcr.io as %s\n", githubUsername)
			return
		}
		fmt.Printf("Failed to login with environment variables: %v\n", err)
	}

	fmt.Println("Warning: Could not login to ghcr.io. Continuing anyway...")
}

func Build(service string, compose ComposeInfo, noCache bool) {
	cmd := fmt.Sprintf("%s build", mainCommand(compose))
	if noCache {
		cmd = fmt.Sprintf("%s --no-cache", cmd)
	}

	ghcrLogin()
	ecrLogin()
	RunCommand("%s %s", cmd, service)
}

// Up bring up the Docker containers
func Up(compose ComposeInfo) {
	str := serviceString(compose, "up")
	ghcrLogin()
	ecrLogin()
	RunCommand("%s up -d %s", mainCommand(compose), str)
}

// Down bring down the Docker containers
func Down(compose ComposeInfo, service string) {
	if len(compose.RequestedServices) > 0 {
		fmt.Printf("Requested services ,%v", compose.RequestedServices[0])
		var command string
		if service != "" {
			command = service
		} else {
			command = serviceString(compose, "down")
		}
		RunCommand("%s stop %s", mainCommand(compose), command)
		RunCommand("%s rm -f %s", mainCommand(compose), command)
	} else {
		RunCommand("%s down", mainCommand(compose))
	}
}

// Stop the Docker containers
func Stop(compose ComposeInfo) {
	if len(compose.RequestedServices) > 0 {
		fmt.Printf("Requested services ,%v", compose.RequestedServices[0])
		RunCommand("%s stop %s", mainCommand(compose), serviceString(compose, "stop"))
	} else {
		RunCommand("%s stop", mainCommand(compose))
	}
}

// Logs show the logs for the selected containers
func Logs(compose ComposeInfo, service string) {
	var command string
	if service != "" {
		command = service
	} else {
		command = serviceString(compose, "logs")
	}
	RunCommand("%s logs -f %s", mainCommand(compose), command)
}

// Ps show the currently running containers
func Ps(compose ComposeInfo) {
	RunCommand("%s ps", mainCommand(compose))
}

// Exec execute a command against a service
func Exec(compose ComposeInfo, service string, command []string) {
	validateService(compose, "exec", service)
	executeDockerCommand(compose, service, strings.Join(command, " "), "")
}

// Mysql start a mysql client
func Mysql(compose ComposeInfo, input string) {
	// check which version is running
	versions := []string{"mysql56", "mysql57", "mysql8"}
	for _, version := range versions {
		if compose.IsServiceRequested(version) {
			executeDockerCommand(compose, version, "mysql", input)
			return
		}
	}

	// not found
	Exit("mysql service not provided! Please use the --services option!")

}

// RedisCLI starts up the Redis command line
func RedisCLI(compose ComposeInfo) {
	executeDockerCommand(compose, "redis", "redis-cli", "")
}

// Config print docker compose config
func Config(compose ComposeInfo) {
	RunCommand("%s config", mainCommand(compose))
}
