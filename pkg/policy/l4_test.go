// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of Cilium

package policy

import (
	"bytes"
	"encoding/json"
	"sort"

	"github.com/kr/pretty"
	. "gopkg.in/check.v1"

	"github.com/cilium/cilium/api/v1/models"
	"github.com/cilium/cilium/pkg/checker"
	"github.com/cilium/cilium/pkg/labels"
	"github.com/cilium/cilium/pkg/policy/api"
)

func (s *PolicyTestSuite) TestCreateL4Filter(c *C) {
	tuple := api.PortProtocol{Port: "80", Protocol: api.ProtoTCP}
	portrule := &api.PortRule{
		Ports: []api.PortProtocol{tuple},
		Rules: &api.L7Rules{
			HTTP: []api.PortRuleHTTP{
				{Path: "/public", Method: "GET"},
			},
		},
	}
	selectors := []api.EndpointSelector{
		api.NewESFromLabels(),
		api.NewESFromLabels(labels.ParseSelectLabel("bar")),
	}

	for _, selector := range selectors {
		eps := []api.EndpointSelector{selector}
		// Regardless of ingress/egress, we should end up with
		// a single L7 rule whether the selector is wildcarded
		// or if it is based on specific labels.
		filter, err := createL4IngressFilter(testPolicyContext, eps, nil, portrule, tuple, tuple.Protocol, nil)
		c.Assert(err, IsNil)
		c.Assert(len(filter.L7RulesPerSelector), Equals, 1)
		c.Assert(filter.IsEnvoyRedirect(), Equals, true)
		c.Assert(filter.IsProxylibRedirect(), Equals, false)

		filter, err = createL4EgressFilter(testPolicyContext, eps, portrule, tuple, tuple.Protocol, nil, nil)
		c.Assert(err, IsNil)
		c.Assert(len(filter.L7RulesPerSelector), Equals, 1)
		c.Assert(filter.IsEnvoyRedirect(), Equals, true)
		c.Assert(filter.IsProxylibRedirect(), Equals, false)
	}
}

func (s *PolicyTestSuite) TestCreateL4FilterMissingSecret(c *C) {
	tuple := api.PortProtocol{Port: "80", Protocol: api.ProtoTCP}
	portrule := &api.PortRule{
		Ports: []api.PortProtocol{tuple},
		TerminatingTLS: &api.TLSContext{
			Secret: &api.Secret{
				Name: "notExisting",
			},
		},
		Rules: &api.L7Rules{
			HTTP: []api.PortRuleHTTP{
				{Path: "/public", Method: "GET"},
			},
		},
	}
	selectors := []api.EndpointSelector{
		api.NewESFromLabels(),
		api.NewESFromLabels(labels.ParseSelectLabel("bar")),
	}

	for _, selector := range selectors {
		eps := []api.EndpointSelector{selector}
		// Regardless of ingress/egress, we should end up with
		// a single L7 rule whether the selector is wildcarded
		// or if it is based on specific labels.
		_, err := createL4IngressFilter(testPolicyContext, eps, nil, portrule, tuple, tuple.Protocol, nil)
		c.Assert(err, Not(IsNil))

		_, err = createL4EgressFilter(testPolicyContext, eps, portrule, tuple, tuple.Protocol, nil, nil)
		c.Assert(err, Not(IsNil))
	}
}

type SortablePolicyRules []*models.PolicyRule

func (a SortablePolicyRules) Len() int           { return len(a) }
func (a SortablePolicyRules) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a SortablePolicyRules) Less(i, j int) bool { return a[i].Rule < a[j].Rule }

func (s *PolicyTestSuite) TestJSONMarshal(c *C) {
	model := &models.L4Policy{}
	c.Assert(pretty.Sprintf("%+ v", model.Egress), checker.DeepEquals, "[]")
	c.Assert(pretty.Sprintf("%+ v", model.Ingress), checker.DeepEquals, "[]")

	policy := L4Policy{
		Egress: L4PolicyMap{
			"8080/TCP": {
				Port:     8080,
				Protocol: api.ProtoTCP,
				Ingress:  false,
			},
		},
		Ingress: L4PolicyMap{
			"80/TCP": {
				Port: 80, Protocol: api.ProtoTCP,
				L7Parser: "http",
				L7RulesPerSelector: L7DataMap{
					cachedFooSelector: &PerSelectorPolicy{
						L7Rules: api.L7Rules{
							HTTP: []api.PortRuleHTTP{{Path: "/", Method: "GET"}},
						},
					},
				},
				Ingress: true,
			},
			"9090/TCP": {
				Port: 9090, Protocol: api.ProtoTCP,
				L7Parser: "tester",
				L7RulesPerSelector: L7DataMap{
					cachedFooSelector: &PerSelectorPolicy{
						L7Rules: api.L7Rules{
							L7Proto: "tester",
							L7: []api.PortRuleL7{
								map[string]string{
									"method": "PUT",
									"path":   "/"},
								map[string]string{
									"method": "GET",
									"path":   "/"},
							},
						},
					},
				},
				Ingress: true,
			},
			"8080/TCP": {
				Port: 8080, Protocol: api.ProtoTCP,
				L7Parser: "http",
				L7RulesPerSelector: L7DataMap{
					cachedFooSelector: &PerSelectorPolicy{
						L7Rules: api.L7Rules{
							HTTP: []api.PortRuleHTTP{
								{Path: "/", Method: "GET"},
								{Path: "/bar", Method: "GET"},
							},
						},
					},
					wildcardCachedSelector: &PerSelectorPolicy{
						L7Rules: api.L7Rules{
							HTTP: []api.PortRuleHTTP{{Path: "/", Method: "GET"}},
						},
					},
				},
				Ingress: true,
			},
		},
	}

	model = policy.GetModel()
	c.Assert(model, NotNil)

	expectedEgress := []string{`{
  "port": 8080,
  "protocol": "TCP"
}`}
	sort.StringSlice(expectedEgress).Sort()
	sort.Sort(SortablePolicyRules(model.Egress))
	c.Assert(len(expectedEgress), Equals, len(model.Egress))
	for i := range expectedEgress {
		expected := new(bytes.Buffer)
		err := json.Compact(expected, []byte(expectedEgress[i]))
		c.Assert(err, Equals, nil, Commentf("Could not compact expected json"))
		c.Assert(model.Egress[i].Rule, Equals, expected.String())
	}

	expectedIngress := []string{`{
  "port": 80,
  "protocol": "TCP",
  "l7-rules": [
    {
      "\u0026LabelSelector{MatchLabels:map[string]string{any.foo: ,},MatchExpressions:[]LabelSelectorRequirement{},}": {
        "http": [
          {
            "path": "/",
            "method": "GET"
          }
        ]
      }
    }
  ]
}`,
		`{
  "port": 9090,
  "protocol": "TCP",
  "l7-rules": [
    {
      "\u0026LabelSelector{MatchLabels:map[string]string{any.foo: ,},MatchExpressions:[]LabelSelectorRequirement{},}": {
        "l7proto": "tester",
        "l7": [
          {
            "method": "PUT",
            "path": "/"
          },
          {
            "method": "GET",
            "path": "/"
          }
        ]
      }
    }
  ]
}`,
		`{
  "port": 8080,
  "protocol": "TCP",
  "l7-rules": [
    {
      "\u0026LabelSelector{MatchLabels:map[string]string{any.foo: ,},MatchExpressions:[]LabelSelectorRequirement{},}": {
        "http": [
          {
            "path": "/",
            "method": "GET"
          },
          {
            "path": "/bar",
            "method": "GET"
          }
        ]
      }
    },
    {
      "\u0026LabelSelector{MatchLabels:map[string]string{},MatchExpressions:[]LabelSelectorRequirement{},}": {
        "http": [
          {
            "path": "/",
            "method": "GET"
          }
        ]
      }
    }
  ]
}`}
	sort.StringSlice(expectedIngress).Sort()
	sort.Sort(SortablePolicyRules(model.Ingress))
	c.Assert(len(expectedIngress), Equals, len(model.Ingress))
	for i := range expectedIngress {
		expected := new(bytes.Buffer)
		err := json.Compact(expected, []byte(expectedIngress[i]))
		c.Assert(err, Equals, nil, Commentf("Could not compact expected json"))
		c.Assert(model.Ingress[i].Rule, Equals, expected.String())
	}

	c.Assert(policy.HasEnvoyRedirect(), Equals, true)
	c.Assert(policy.HasProxylibRedirect(), Equals, true)
}
