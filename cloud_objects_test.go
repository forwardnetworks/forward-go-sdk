package forward

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNetworksCloudObjects(t *testing.T) {
	t.Parallel()

	var uris []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uris = append(uris, r.URL.RequestURI())
		switch r.URL.Path {
		case "/api/networks/N1/cloud-objects":
			_, _ = io.WriteString(w, `{"cloudObjects":[
			  {"id":"rtb-1","name":"main","accountName":"Demo","type":"ROUTE_TABLE","vpcs":["vpc-9"],"locationIds":["aws:us-east-1"],"ipv4Blocks":["10.0.0.0/16"]},
			  {"id":"sg-2","accountName":"Demo","type":"SECURITY_GROUP"}],"totalCloudObjects":41}`)
		case "/api/networks/N1/cloud-objects/rtb-1":
			_, _ = io.WriteString(w, `{"cloudType":"AWS","type":"ROUTE_TABLE","id":"rtb-1","name":"main","coveredTopo":{"devices":["r1"]},"deviceInfo":{"name":"rtb-1"}}`)
		case "/api/networks/N1/cloud-objects/rtb-1/files":
			_, _ = io.WriteString(w, `[{"id":"77","fileName":"rtb-1.json","size":512}]`)
		default:
			t.Errorf("unexpected %s", r.URL.RequestURI())
		}
	}))
	defer server.Close()
	networks := newTestClient(t, server.URL).Networks
	ctx := context.Background()

	page, _, err := networks.CloudObjects(ctx, "N1", CloudObjectListOptions{SnapshotID: "13961", Offset: 20, Limit: 10})
	if err != nil || uris[0] != "/api/networks/N1/cloud-objects?limit=10&offset=20&snapshotId=13961" {
		t.Fatalf("CloudObjects: %v %s", err, uris[0])
	}
	if page.Total != 41 || len(page.CloudObjects) != 2 || page.CloudObjects[0].ID != "rtb-1" || page.CloudObjects[0].VPCs[0] != "vpc-9" || page.CloudObjects[1].Name != "" {
		t.Fatalf("page = %+v", page)
	}
	if string(page.CloudObjects[0].Raw) == "" || !strings.Contains(string(page.CloudObjects[0].Raw), "ipv4Blocks") || !strings.Contains(string(page.CloudObjects[0].Raw), "aws:us-east-1") {
		t.Fatalf("provider fields and location ids must stay reachable in Raw: %s", page.CloudObjects[0].Raw)
	}
	if _, _, err := networks.CloudObjects(ctx, "N1", CloudObjectListOptions{}); err != nil || uris[1] != "/api/networks/N1/cloud-objects" {
		t.Fatalf("no options must send no parameter: %v %s", err, uris[1])
	}
	info, _, err := networks.CloudObject(ctx, "N1", "rtb-1", "")
	if err != nil || info.CloudType != "AWS" || string(info.CoveredTopology) != `{"devices":["r1"]}` || info.DeviceInfo == nil {
		t.Fatalf("CloudObject() = %+v, %v", info, err)
	}
	files, _, err := networks.CloudObjectFiles(ctx, "N1", "rtb-1", CloudObjectFilesOptions{AllFiles: true})
	if err != nil || len(files) != 1 || files[0].ID != "77" || files[0].FileName != "rtb-1.json" || uris[len(uris)-1] != "/api/networks/N1/cloud-objects/rtb-1/files?generatedOnly=false" {
		t.Fatalf("CloudObjectFiles() = %+v, %v; %s", files, err, uris[len(uris)-1])
	}
	for name, f := range map[string]func() error{
		"negative offset": func() error {
			_, _, err := networks.CloudObjects(ctx, "N1", CloudObjectListOptions{Offset: -1})
			return err
		},
		"blank object": func() error { _, _, err := networks.CloudObject(ctx, "N1", " ", ""); return err },
		"blank files id": func() error {
			_, _, err := networks.CloudObjectFiles(ctx, "N1", "", CloudObjectFilesOptions{})
			return err
		},
	} {
		if err := f(); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
}
