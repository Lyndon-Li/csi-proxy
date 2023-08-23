package integrationtests

import (
	"context"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kubernetes-csi/csi-proxy/client/api/disk/v1beta1"
	diskv1beta1client "github.com/kubernetes-csi/csi-proxy/client/groups/disk/v1beta1"
	"github.com/stretchr/testify/require"
)

func v1beta1DiskTests(t *testing.T) {
	t.Run("ListDiskIDs,ListDiskLocations", func(t *testing.T) {
		// even though this test doesn't need the VHD API it failed in Github Actions
		// see https://github.com/kubernetes-csi/csi-proxy/pull/140/checks?check_run_id=2671787129
		skipTestOnCondition(t, isRunningOnGhActions())

		client, err := diskv1beta1client.NewClient()
		require.Nil(t, err)
		defer client.Close()

		listRequest := &v1beta1.ListDiskIDsRequest{}
		diskIDsResponse, err := client.ListDiskIDs(context.TODO(), listRequest)
		require.Nil(t, err)

		// example output for GCE (0 is ok, others are virtual disks)
		// diskIDs:{key:0  value:{page83:"Google  persistent-disk-0"  serial_number:"                    "}}
		// diskIDs:{key:1  value:{page83:"4d53465420202020328d59b360875845ac645473be8267bf"}}
		// diskIDs:{key:2  value:{page83:"4d534654202020208956a91dadfe3d48865f9b9bcbdb8d3e"}}
		// diskIDs:{key:3  value:{page83:"4d534654202020207a3d18d72787ee47bdc127cb4f06403a"}}
		t.Logf("diskIDsResponse=%v", diskIDsResponse)

		cmd := "hostname"
		hostname, err := runPowershellCmd(t, cmd)
		if err != nil {
			t.Errorf("Error: %v. Command: %s. Out: %s", err, cmd, hostname)
		}

		hostname = strings.TrimSpace(hostname)
		diskIDsMap := diskIDsResponse.DiskIDs
		if len(diskIDsMap) == 0 {
			t.Errorf("Expected to get at least one diskIDs, instead got diskIDsResponse.DiskIDs=%+v", diskIDsMap)
		}

		listDiskLocationsRequest := &v1beta1.ListDiskLocationsRequest{}
		listDiskLocationsResponse, err := client.ListDiskLocations(context.TODO(), listDiskLocationsRequest)
		require.Nil(t, err)
		t.Logf("listDiskLocationsResponse=%v", listDiskLocationsResponse)
		if len(listDiskLocationsResponse.DiskLocations) == 0 {
			t.Errorf("Expected to get at least one diskLocation, instead got DiskLocations=%+v", listDiskLocationsResponse.DiskLocations)
		}
	})

	t.Run("Get/SetDiskState", func(t *testing.T) {
		client, err := diskv1beta1client.NewClient()
		require.NoError(t, err)

		defer client.Close()

		// initialize disk
		vhd, vhdCleanup := diskInit(t)
		defer vhdCleanup()

		diskID := strconv.FormatUint(uint64(vhd.DiskNumber), 10)

		// disk stats
		diskStatsRequest := &v1beta1.DiskStatsRequest{
			DiskID: diskID,
		}
		diskStatsResponse, err := client.DiskStats(context.TODO(), diskStatsRequest)
		require.NoError(t, err)
		if !sizeIsAround(t, diskStatsResponse.DiskSize, vhd.InitialSize) {
			t.Fatalf("DiskStats doesn't have the expected size, wanted (close to)=%d got=%d", vhd.InitialSize, diskStatsResponse.DiskSize)
		}

		// Rescan
		_, err = client.Rescan(context.TODO(), &v1beta1.RescanRequest{})
		require.NoError(t, err)
	})

	t.Run("PartitionDisk", func(t *testing.T) {
		var err error
		client, err := diskv1beta1client.NewClient()
		require.NoError(t, err)
		defer client.Close()

		// initialize disk but don't partition it using `diskInit`
		s1 := rand.NewSource(time.Now().UTC().UnixNano())
		r1 := rand.New(s1)

		testPluginPath := fmt.Sprintf("C:\\var\\lib\\kubelet\\plugins\\testplugin-%d.csi.io\\", r1.Intn(100))
		mountPath := fmt.Sprintf("%smount-%d", testPluginPath, r1.Intn(100))
		vhdxPath := fmt.Sprintf("%sdisk-%d.vhdx", testPluginPath, r1.Intn(100))

		var cmd, out string
		const initialSize = 1 * 1024 * 1024 * 1024
		const partitionStyle = "GPT"

		cmd = fmt.Sprintf("mkdir %s", mountPath)
		if out, err = runPowershellCmd(t, cmd); err != nil {
			t.Fatalf("Error: %v. Command: %q. Out: %s", err, cmd, out)
		}
		cmd = fmt.Sprintf("New-VHD -Path %s -SizeBytes %d", vhdxPath, initialSize)
		if out, err = runPowershellCmd(t, cmd); err != nil {
			t.Fatalf("Error: %v. Command: %q. Out: %s.", err, cmd, out)
		}
		cmd = fmt.Sprintf("Mount-VHD -Path %s", vhdxPath)
		if out, err = runPowershellCmd(t, cmd); err != nil {
			t.Fatalf("Error: %v. Command: %q. Out: %s", err, cmd, out)
		}

		var diskNumUnparsed string
		cmd = fmt.Sprintf("(Get-VHD -Path %s).DiskNumber", vhdxPath)
		if diskNumUnparsed, err = runPowershellCmd(t, cmd); err != nil {
			t.Fatalf("Error: %v. Command: %s", err, cmd)
		}

		// make disk partition request
		diskPartitionRequest := &v1beta1.PartitionDiskRequest{
			DiskID: strings.TrimSpace(diskNumUnparsed),
		}
		_, err = client.PartitionDisk(context.TODO(), diskPartitionRequest)
		require.NoError(t, err)
	})
}
