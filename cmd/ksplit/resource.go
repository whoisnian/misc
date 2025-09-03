package main

import (
	"strings"

	"sigs.k8s.io/kustomize/api/types"
	"sigs.k8s.io/kustomize/kyaml/resid"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

// kubectl api-resources
var shortNameMap = map[string]string{
	"ComponentStatus":           "cs",
	"ConfigMap":                 "cm",
	"Endpoints":                 "ep",
	"Event":                     "ev",
	"LimitRange":                "limits",
	"Namespace":                 "ns",
	"Node":                      "no",
	"PersistentVolumeClaim":     "pvc",
	"PersistentVolume":          "pv",
	"Pod":                       "po",
	"ReplicationController":     "rc",
	"ResourceQuota":             "quota",
	"ServiceAccount":            "sa",
	"Service":                   "svc",
	"CustomResourceDefinition":  "crd",
	"DaemonSet":                 "ds",
	"Deployment":                "deploy",
	"ReplicaSet":                "rs",
	"StatefulSet":               "sts",
	"HorizontalPodAutoscaler":   "hpa",
	"CronJob":                   "cj",
	"CertificateSigningRequest": "csr",
	"GatewayClass":              "gc",
	"Gateway":                   "gtw",
	"ReferenceGrant":            "refgrant",
	"Ingress":                   "ing",
	"NetworkPolicy":             "netpol",
	"PodDisruptionBudget":       "pdb",
	"PriorityClass":             "pc",
	"ElasticQuota":              "eq",
	"ElasticQuotaTree":          "eqtree",
	"PodGroup":                  "pg",
	"StorageClass":              "sc",
}

func ShortName(kind string) string {
	if shortName, ok := shortNameMap[kind]; ok {
		return shortName
	}
	return strings.ToLower(kind)
}

// https://github.com/kubernetes-sigs/kustomize/blob/cd30471046d33b64b3d761d22c63365387dccd02/api/krusty/kustomizer.go#L129
// https://github.com/kubernetes-sigs/kustomize/blob/cd30471046d33b64b3d761d22c63365387dccd02/api/internal/builtins/SortOrderTransformer.go
// https://github.com/kubernetes-sigs/kustomize/blob/cd30471046d33b64b3d761d22c63365387dccd02/kyaml/resid/resid.go
// https://github.com/kubernetes-sigs/kustomize/blob/cd30471046d33b64b3d761d22c63365387dccd02/kyaml/resid/gvk.go
var defaultOrderFirst = []string{ //nolint:gochecknoglobals
	"Namespace",
	"ResourceQuota",
	"StorageClass",
	"CustomResourceDefinition",
	"ServiceAccount",
	"PodSecurityPolicy",
	"Role",
	"ClusterRole",
	"RoleBinding",
	"ClusterRoleBinding",
	"ConfigMap",
	"Secret",
	"Endpoints",
	"Service",
	"LimitRange",
	"PriorityClass",
	"PersistentVolume",
	"PersistentVolumeClaim",
	"Deployment",
	"StatefulSet",
	"CronJob",
	"PodDisruptionBudget",
}
var defaultOrderLast = []string{ //nolint:gochecknoglobals
	"MutatingWebhookConfiguration",
	"ValidatingWebhookConfiguration",
}
var typeOrders = func() map[string]int {
	m := map[string]int{}
	for i, n := range defaultOrderFirst {
		m[n] = -len(defaultOrderFirst) + i
	}
	for i, n := range defaultOrderLast {
		m[n] = 1 + i
	}
	return m
}()

func NodeIsLessThan(a, b *yaml.RNode) bool {
	gvk1, gvk2 := resid.GvkFromNode(a), resid.GvkFromNode(b)
	if !gvk1.Equals(gvk2) {
		return gvkLessThan(gvk1, gvk2)
	}
	return legacyResIDSortString(a, gvk1) < legacyResIDSortString(b, gvk2)
}

func gvkLessThan(gvk1, gvk2 resid.Gvk) bool {
	index1 := typeOrders[gvk1.Kind]
	index2 := typeOrders[gvk2.Kind]
	if index1 != index2 {
		return index1 < index2
	}
	if (gvk1.Kind == types.NamespaceKind && gvk2.Kind == types.NamespaceKind) && (gvk1.Group == "" || gvk2.Group == "") {
		return legacyGVKSortString(gvk1) > legacyGVKSortString(gvk2)
	}
	return legacyGVKSortString(gvk1) < legacyGVKSortString(gvk2)
}

func legacyGVKSortString(x resid.Gvk) string {
	legacyNoGroup := "~G"
	legacyNoVersion := "~V"
	legacyNoKind := "~K"
	legacyFieldSeparator := "_"

	g := x.Group
	if g == "" {
		g = legacyNoGroup
	}
	v := x.Version
	if v == "" {
		v = legacyNoVersion
	}
	k := x.Kind
	if k == "" {
		k = legacyNoKind
	}
	return strings.Join([]string{g, v, k}, legacyFieldSeparator)
}

func legacyResIDSortString(node *yaml.RNode, gvk resid.Gvk) string {
	legacyNoNamespace := "~X"
	legacyNoName := "~N"
	legacySeparator := "|"

	ns := node.GetNamespace()
	if ns == "" {
		ns = legacyNoNamespace
	}
	nm := node.GetName()
	if nm == "" {
		nm = legacyNoName
	}
	return strings.Join([]string{gvk.String(), ns, nm}, legacySeparator)
}
