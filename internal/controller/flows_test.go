package controller

import (
	"fmt"

	"github.com/Mellanox/spectrum-x-operator/pkg/exec"
	gomock "github.com/golang/mock/gomock"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Flows", func() {
	var (
		flows    *Flows
		execMock *exec.MockAPI
		ctrl     *gomock.Controller
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		execMock = exec.NewMockAPI(ctrl)
		flows = &Flows{
			Exec: execMock,
		}
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Context("DeletePodRailFlows", func() {
		It("should delete flows on pod deletion", func() {
			execMock.EXPECT().Execute("ovs-ofctl del-flows br-rail1 cookie=0x5/-1").Return("", nil)
			err := flows.DeletePodRailFlows(0x5, "br-rail1")
			Expect(err).Should(Succeed())
		})

		It("should return error if ovs-ofctl fails", func() {
			execMock.EXPECT().Execute("ovs-ofctl del-flows br-rail1 cookie=0x5/-1").Return("", fmt.Errorf("failed to delete flows"))
			err := flows.DeletePodRailFlows(0x5, "br-rail1")
			Expect(err).Should(HaveOccurred())
		})
	})
})
