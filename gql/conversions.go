package gql

import "github.com/superfly/flyctl/api"

// ToAppCompact converts the genqclient AppFragment to an AppCompact suitable for flaps, which only needs two fields
func ToAppCompact(app AppData) *api.AppCompact {
	return &api.AppCompact{
		Name: app.Name,
		Organization: &api.OrganizationBasic{
			Slug: app.Organization.Slug,
		},
	}
}
