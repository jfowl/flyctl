package logs

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/spf13/cobra"
	"github.com/superfly/flyctl/api"
	"github.com/superfly/flyctl/client"
	"github.com/superfly/flyctl/flaps"
	"github.com/superfly/flyctl/gql"
	"github.com/superfly/flyctl/internal/appconfig"
	"github.com/superfly/flyctl/internal/command"
	"github.com/superfly/flyctl/internal/command/deploy"
	"github.com/superfly/flyctl/internal/flag"
	"github.com/superfly/flyctl/internal/prompt"
	"github.com/superfly/flyctl/iostreams"
	"github.com/vektah/gqlparser/gqlerror"
)

type Provider struct {
	Slug         string
	Name         string
	Auto         bool
	RequriedVars []string
	OptionalVars []string
}

var providers = []Provider{
	{
		Slug: "aws_s3",
		Name: "AWS S3",
		Auto: false,
		RequriedVars: []string{
			"AWS_BUCKET", "AWS_REGION", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY",
		},
		OptionalVars: []string{
			"S3_ENDPOINT",
		},
	},
	{
		Slug: "axiom",
		Name: "Axiom",
		Auto: false,
		RequriedVars: []string{
			"AXIOM_TOKEN", "AXIOM_DATASET",
		},
	},
	{
		Slug: "datadog",
		Name: "Datadog",
		Auto: false,
		RequriedVars: []string{
			"DATADOG_API_KEY",
		},
		OptionalVars: []string{
			"DATADOG_SITE",
		},
	},
	{
		Slug: "erasearch",
		Name: "Erasearch",
		Auto: false,
		RequriedVars: []string{
			"ERASEARCH_URL", "ERASEARCH_INDEX", "ERASEARCH_AUTH",
		},
	},
	{
		Slug: "honeycomb",
		Name: "Honeycomb",
		Auto: false,
		RequriedVars: []string{
			"HONEYCOMB_API_KEY", "HONEYCOMB_DATASET",
		},
	},
	{
		Slug: "http",
		Name: "HTTP",
		Auto: false,
		RequriedVars: []string{
			"HTTP_URL", "HTTP_TOKEN",
		},
	},
	{
		Slug: "humio",
		Name: "Humio",
		Auto: false,
		RequriedVars: []string{
			"HUMIO_TOKEN",
		},
	},
	{
		Slug: "mezmo",
		Name: "Mezmo",
		Auto: false,
		RequriedVars: []string{
			"MEZMO_API_KEY",
		},
	},
	{
		Slug: "logflare",
		Name: "Logflare",
		Auto: false,
		RequriedVars: []string{
			"LOGFLARE_API_KEY", "LOGFLARE_SOURCE_TOKEN",
		},
	},
	{
		Slug: "logtail",
		Name: "Logtail",
		Auto: true,
	},
	{
		Slug: "loki",
		Name: "Loki",
		Auto: false,
		RequriedVars: []string{
			"LOKI_URL", "LOKI_USERNAME", "LOKI_PASSWORD",
		},
	},
	{
		Slug: "new_relic",
		Name: "New Relic",
		Auto: false,
		RequriedVars: []string{
			"NEW_RELIC_REGION", "NEW_RELIC_ACCOUNT_ID",
		},
		OptionalVars: []string{
			"NEW_RELIC_LICENSE_KEY", "NEW_RELIC_INSERT_KEY",
		},
	},
	{
		Slug: "papertrail",
		Name: "Papertrail",
		Auto: false,
		RequriedVars: []string{
			"PAPERTRAIL_ENDPOINT",
		},
		OptionalVars: []string{
			"PAPERTRAIL_ENCODING_CODEC",
		},
	},
	{
		Slug: "sematext",
		Name: "Sematext",
		Auto: false,
		RequriedVars: []string{
			"SEMATEXT_REGION", "SEMATEXT_TOKEN",
		},
	},
	{
		Slug: "uptrace",
		Name: "Uptrace",
		Auto: false,
		RequriedVars: []string{
			"UPTRACE_API_KEY", "UPTRACE_PROJECT",
		},
		OptionalVars: []string{
			"UPTRACE_SINK_INPUT", "UPTRACE_SINK_ENCODING",
		},
	},
}

func newShip() (cmd *cobra.Command) {
	const (
		short = "Ship application logs to log providers"
		long  = short + "\n"
	)

	cmd = command.New("ship", short, long, runSetup, command.RequireSession, command.RequireAppName)
	flag.Add(cmd,
		flag.App(),
		flag.AppConfig(),
	)
	return cmd
}

func runSetup(ctx context.Context) (err error) {
	client := client.FromContext(ctx).API().GenqClient
	appName := appconfig.NameFromContext(ctx)

	if err != nil {
		return err
	}

	// Fetch the target organization from the app
	appNameResponse, err := gql.GetApp(ctx, client, appName)
	if err != nil {
		return err
	}

	targetApp := appNameResponse.App.AppData
	targetOrg := targetApp.Organization

	var providerOptions []string

	for _, provider := range providers {
		providerOptions = append(providerOptions, provider.Name)
	}

	var providerIndex int

	err = prompt.Select(ctx, &providerIndex, "Select a logging provider:", "", providerOptions...)

	if err != nil {
		return err
	}

	if providers[providerIndex].Auto {
		ProvisionAutoShipper(ctx, targetApp, providers[providerIndex].Slug)
	} else {
		//	ProvisionGlobalShipper(ctx, targetApp, providers[providerIndex].Slug)
	}

	// Ensure we have an app with the log-shipper role
	shipperApp, err := EnsureShipperApp(ctx, targetOrg)

	if err != nil {
		return err
	}

	// Ensure we have a running log shipper VM
	err = EnsureShipperMachine(ctx, shipperApp)

	// Set NATS token macaroon whose only permission is to read logs from the entire organization
	SetNatsTokenSecret(ctx, shipperApp)

	// Set log provider secrets

	return
}

func ProvisionAutoShipper(ctx context.Context, targetApp gql.AppData, provider string) (token string, err error) {
	client := client.FromContext(ctx).API().GenqClient
	addOnName := targetApp.Name + "-log-shipper"

	getAddOnResponse, err := gql.GetAddOn(ctx, client, addOnName)

	errType := reflect.TypeOf(err)
	fmt.Println("Error type:", errType)

	for err != nil {
		if errList, ok := err.(gqlerror.List); ok {
			for _, gqlErr := range errList {
				fmt.Println(gqlErr)
			}
			input := gql.CreateAddOnInput{
				OrganizationId: targetApp.Organization.Id,
				Name:           addOnName,
				AppId:          targetApp.Id,
				Type:           gql.AddOnTypes[provider],
			}

			createAddOnResponse, err := gql.CreateAddOn(ctx, client, input)

			if err != nil {
				return "", err
			}

			token = createAddOnResponse.CreateAddOn.AddOn.Token

			break

		} else {
			// return "", err
		}

		err = errors.Unwrap(err)
	}

	if err == nil {
		fmt.Println("already provisioned", getAddOnResponse.AddOn.Organization.Slug)
		token = getAddOnResponse.AddOn.Token
	}

	return token, nil

}

func LoggerAppName(prefix string) string {
	return prefix + "-auto-log-shipper"
}

func LoggerAddOnName(appName string, provider string) string {
	return appName + "-" + provider
}

func EnsureShipperApp(ctx context.Context, targetOrg gql.AppDataOrganization) (shipperApp *gql.AppData, err error) {
	client := client.FromContext(ctx).API().GenqClient
	appsResult, err := gql.GetAppsByRole(ctx, client, "log-shipper", targetOrg.Id)

	if err != nil {
		return nil, err
	}

	if len(appsResult.Apps.Nodes) > 0 {
		shipperApp = &appsResult.Apps.Nodes[0].AppData
	} else {
		input := gql.DefaultCreateAppInput()
		input.Machines = true
		input.OrganizationId = targetOrg.Id
		input.AppRoleId = "log-shipper"
		input.Name = LoggerAppName(targetOrg.RawSlug)

		createdAppResult, err := gql.CreateApp(ctx, client, input)
		if err != nil {
			return nil, err
		}

		shipperApp = &createdAppResult.CreateApp.App.AppData
		err = EnsureShipperMachine(ctx, shipperApp)

		if err != nil {
			return nil, err
		}
	}

	return
}

func EnsureShipperMachine(ctx context.Context, shipperApp *gql.AppData) (err error) {
	client := client.FromContext(ctx).API().GenqClient
	io := iostreams.FromContext(ctx)

	flapsClient, err := flaps.New(ctx, gql.ToAppCompact(*shipperApp))

	if err != nil {
		return err
	}

	machines, err := flapsClient.List(ctx, "")
	if err != nil {
		return err
	}

	if len(machines) > 0 {
		return
	}

	machineConf := &api.MachineConfig{
		Guest: &api.MachineGuest{
			CPUKind:  "shared",
			CPUs:     1,
			MemoryMB: 256,
		},
		Image: "flyio/log-shipper:auto-a14aa63",
		Metadata: map[string]string{
			api.MachineConfigMetadataKeyFlyPlatformVersion: api.MachineFlyPlatformVersion2,
			api.MachineConfigMetadataKeyFlyManagedPostgres: "true",
			"managed-by-fly-deploy":                        "true",
		},
	}

	launchInput := api.LaunchMachineInput{
		Name:   "log-shipper",
		Config: machineConf,
	}

	regionResponse, err := gql.GetNearestRegion(ctx, client)
	if err != nil {
		return err
	}

	launchInput.Region = regionResponse.NearestRegion.Code

	_, err = flapsClient.Launch(ctx, launchInput)

	if err != nil {
		return err
	}

	fmt.Fprintf(io.Out, "Launched log shipper app %s\n in the %s region", shipperApp.Name, launchInput.Region)

	return
}

func SetNatsTokenSecret(ctx context.Context, shipperApp *gql.AppData) (err error) {
	client := client.FromContext(ctx).API().GenqClient

	tokenResponse, err := gql.CreateLimitedAccessToken(ctx, client, shipperApp.Organization.Slug+"-logs", shipperApp.Organization.Id, "read_organization_apps", "", "")

	if err != nil {
		return
	}

	gql.SetSecrets(ctx, client, gql.SetSecretsInput{
		AppId: shipperApp.Id,

		Secrets: []gql.SecretInput{
			{
				Key:   "NATS_TOKEN",
				Value: tokenResponse.CreateLimitedAccessToken.LimitedAccessToken.Token,
			},
		},
	})

	md, err := deploy.NewMachineDeployment(ctx, deploy.MachineDeploymentArgs{
		AppCompact:       gql.ToAppCompact(*shipperApp),
		RestartOnly:      true,
		SkipHealthChecks: true,
	})

	if err != nil {
		return err
	}
	err = md.DeployMachinesApp(ctx)

	return
}
