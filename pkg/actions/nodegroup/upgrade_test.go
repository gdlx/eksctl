package nodegroup_test

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/cloudformation"
	awseks "github.com/aws/aws-sdk-go/service/eks"
	"github.com/aws/aws-sdk-go/service/ssm"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/weaveworks/eksctl/pkg/actions/nodegroup"
	api "github.com/weaveworks/eksctl/pkg/apis/eksctl.io/v1alpha5"
	"github.com/weaveworks/eksctl/pkg/cfn/manager"
	"github.com/weaveworks/eksctl/pkg/cfn/manager/fakes"
	"github.com/weaveworks/eksctl/pkg/eks"
	"github.com/weaveworks/eksctl/pkg/testutils/mockprovider"
	"github.com/weaveworks/eksctl/pkg/version"
)

var _ = Describe("Upgrade", func() {
	var (
		clusterName, ngName string
		p                   *mockprovider.MockProvider
		cfg                 *api.ClusterConfig
		m                   *nodegroup.Manager
		fakeStackManager    *fakes.FakeStackManager
		fakeClientSet       *fake.Clientset
		options             nodegroup.UpgradeOptions
	)

	BeforeEach(func() {
		ngName = "my-nodegroup"
		clusterName = "my-cluster"
		cfg = api.NewClusterConfig()
		cfg.Metadata.Name = clusterName
		p = mockprovider.NewMockProvider()
		fakeClientSet = fake.NewSimpleClientset()
		m = nodegroup.New(cfg, &eks.ClusterProvider{Provider: p}, fakeClientSet)

		fakeStackManager = new(fakes.FakeStackManager)
		m.SetStackManager(fakeStackManager)
		options = nodegroup.UpgradeOptions{
			NodegroupName:     ngName,
			KubernetesVersion: "1.21",
			Wait:              false,
			ForceUpgrade:      false,
		}
	})

	When("the nodegroup does not have a stack", func() {
		When("launchTemplate Id is set", func() {
			BeforeEach(func() {
				p.MockEKS().On("DescribeNodegroup", &awseks.DescribeNodegroupInput{
					ClusterName:   aws.String(clusterName),
					NodegroupName: aws.String(ngName),
				}).Return(&awseks.DescribeNodegroupOutput{
					Nodegroup: &awseks.Nodegroup{
						NodegroupName: aws.String(ngName),
						ClusterName:   aws.String(clusterName),
						Status:        aws.String("my-status"),
						AmiType:       aws.String("ami-type"),
						Version:       aws.String("1.20"),
						LaunchTemplate: &awseks.LaunchTemplateSpecification{
							Id: aws.String("id-123"),
						},
					},
				}, nil)

				p.MockEKS().On("UpdateNodegroupVersion", &awseks.UpdateNodegroupVersionInput{
					NodegroupName: aws.String(ngName),
					ClusterName:   aws.String(clusterName),
					Force:         aws.Bool(false),
					Version:       aws.String("1.21"),
					LaunchTemplate: &awseks.LaunchTemplateSpecification{
						Id:      aws.String("id-123"),
						Version: aws.String("v2"),
					},
				}).Return(&awseks.UpdateNodegroupVersionOutput{}, nil)
			})

			It("upgrades the nodegroup version and lt by calling the API", func() {
				options.LaunchTemplateVersion = "v2"
				Expect(m.Upgrade(options)).To(Succeed())
			})
		})

		When("launchTemplate Name is set", func() {
			BeforeEach(func() {
				p.MockEKS().On("DescribeNodegroup", &awseks.DescribeNodegroupInput{
					ClusterName:   aws.String(clusterName),
					NodegroupName: aws.String(ngName),
				}).Return(&awseks.DescribeNodegroupOutput{
					Nodegroup: &awseks.Nodegroup{
						NodegroupName: aws.String(ngName),
						ClusterName:   aws.String(clusterName),
						Status:        aws.String("my-status"),
						AmiType:       aws.String("AL2_x86_64"),
						Version:       aws.String("1.20"),
						LaunchTemplate: &awseks.LaunchTemplateSpecification{
							Name: aws.String("lt"),
						},
					},
				}, nil)

				p.MockEKS().On("UpdateNodegroupVersion", &awseks.UpdateNodegroupVersionInput{
					NodegroupName: aws.String(ngName),
					ClusterName:   aws.String(clusterName),
					Force:         aws.Bool(false),
					Version:       aws.String("1.21"),
					LaunchTemplate: &awseks.LaunchTemplateSpecification{
						Name:    aws.String("lt"),
						Version: aws.String("v2"),
					},
				}).Return(&awseks.UpdateNodegroupVersionOutput{}, nil)
			})

			It("upgrades the nodegroup version and lt by calling the API", func() {
				options.LaunchTemplateVersion = "v2"
				Expect(m.Upgrade(options)).To(Succeed())
			})
		})
	})

	When("the nodegroup does have a stack", func() {
		When("ForceUpdateEnabled isn't set", func() {
			When("it uses amazonlinux2", func() {
				BeforeEach(func() {
					fakeStackManager.ListNodeGroupStacksReturns([]manager.NodeGroupStack{{NodeGroupName: ngName}}, nil)

					fakeStackManager.GetManagedNodeGroupTemplateReturns(al2WithoutForceTemplate, nil)

					fakeStackManager.DescribeNodeGroupStackReturns(&manager.Stack{
						Tags: []*cloudformation.Tag{
							{
								Key:   aws.String(api.EksctlVersionTag),
								Value: aws.String(version.GetVersion()),
							},
						},
					}, nil)

					fakeStackManager.UpdateNodeGroupStackReturns(nil)

					p.MockEKS().On("DescribeNodegroup", &awseks.DescribeNodegroupInput{
						ClusterName:   aws.String(clusterName),
						NodegroupName: aws.String(ngName),
					}).Return(&awseks.DescribeNodegroupOutput{
						Nodegroup: &awseks.Nodegroup{
							NodegroupName:  aws.String(ngName),
							ClusterName:    aws.String(clusterName),
							Status:         aws.String("my-status"),
							AmiType:        aws.String("AL2_x86_64"),
							Version:        aws.String("1.20"),
							ReleaseVersion: aws.String("1.20-20201212"),
						},
					}, nil)

					p.MockSSM().On("GetParameter", &ssm.GetParameterInput{
						Name: aws.String("/aws/service/eks/optimized-ami/1.21/amazon-linux-2/recommended/release_version"),
					}).Return(&ssm.GetParameterOutput{
						Parameter: &ssm.Parameter{
							Value: aws.String("1.21-20201212"),
						},
					}, nil)
				})

				It("upgrades the nodegroup with the latest al2 release_version by updating the stack", func() {
					Expect(m.Upgrade(options)).To(Succeed())
					Expect(fakeStackManager.GetManagedNodeGroupTemplateCallCount()).To(Equal(1))
					Expect(fakeStackManager.GetManagedNodeGroupTemplateArgsForCall(0).NodeGroupName).To(Equal(ngName))
					Expect(fakeStackManager.UpdateNodeGroupStackCallCount()).To(Equal(2))
					By("upgrading the ForceUpdateEnabled setting first")
					ng, template, wait := fakeStackManager.UpdateNodeGroupStackArgsForCall(0)
					Expect(ng).To(Equal(ngName))
					Expect(template).To(Equal(al2ForceFalseTemplate))
					Expect(wait).To(BeTrue())

					By("upgrading the ReleaseVersion setting next")
					ng, template, wait = fakeStackManager.UpdateNodeGroupStackArgsForCall(1)
					Expect(ng).To(Equal(ngName))
					Expect(template).To(Equal(al2FullyUpdatedTemplate))
					Expect(wait).To(BeTrue())
				})
			})
		})

		When("it already has ForceUpdateEnabled set to false", func() {
			When("it uses amazonlinux2 GPU nodes", func() {
				BeforeEach(func() {
					fakeStackManager.ListNodeGroupStacksReturns([]manager.NodeGroupStack{{NodeGroupName: ngName}}, nil)

					fakeStackManager.GetManagedNodeGroupTemplateReturns(al2ForceFalseTemplate, nil)

					fakeStackManager.DescribeNodeGroupStackReturns(&manager.Stack{
						Tags: []*cloudformation.Tag{
							{
								Key:   aws.String(api.EksctlVersionTag),
								Value: aws.String(version.GetVersion()),
							},
						},
					}, nil)

					fakeStackManager.UpdateNodeGroupStackReturns(nil)

					p.MockEKS().On("DescribeNodegroup", &awseks.DescribeNodegroupInput{
						ClusterName:   aws.String(clusterName),
						NodegroupName: aws.String(ngName),
					}).Return(&awseks.DescribeNodegroupOutput{
						Nodegroup: &awseks.Nodegroup{
							NodegroupName:  aws.String(ngName),
							ClusterName:    aws.String(clusterName),
							Status:         aws.String("my-status"),
							AmiType:        aws.String("AL2_x86_64_GPU"),
							Version:        aws.String("1.20"),
							ReleaseVersion: aws.String("1.20-20201212"),
						},
					}, nil)

					p.MockSSM().On("GetParameter", &ssm.GetParameterInput{
						Name: aws.String("/aws/service/eks/optimized-ami/1.21/amazon-linux-2-gpu/recommended/release_version"),
					}).Return(&ssm.GetParameterOutput{
						Parameter: &ssm.Parameter{
							Value: aws.String("1.21-20201212"),
						},
					}, nil)
				})

				It("upgrades the nodegroup with the latest al2 release_version by updating the stack", func() {
					Expect(m.Upgrade(options)).To(Succeed())
					Expect(fakeStackManager.GetManagedNodeGroupTemplateCallCount()).To(Equal(1))
					Expect(fakeStackManager.GetManagedNodeGroupTemplateArgsForCall(0).NodeGroupName).To(Equal(ngName))
					Expect(fakeStackManager.UpdateNodeGroupStackCallCount()).To(Equal(1))
					By("upgrading the ReleaseVersion and not updating the ForceUpdateEnabled setting")
					ng, template, wait := fakeStackManager.UpdateNodeGroupStackArgsForCall(0)
					Expect(ng).To(Equal(ngName))
					Expect(template).To(Equal(al2FullyUpdatedTemplate))
					Expect(wait).To(BeTrue())
				})
			})
		})

		When("ForceUpdateEnabled is set to true but the desired value is false", func() {
			When("it uses bottlerocket", func() {
				BeforeEach(func() {
					fakeStackManager.ListNodeGroupStacksReturns([]manager.NodeGroupStack{{NodeGroupName: ngName}}, nil)

					fakeStackManager.GetManagedNodeGroupTemplateReturns(brForceTrueTemplate, nil)

					fakeStackManager.DescribeNodeGroupStackReturns(&manager.Stack{
						Tags: []*cloudformation.Tag{
							{
								Key:   aws.String(api.EksctlVersionTag),
								Value: aws.String(version.GetVersion()),
							},
						},
					}, nil)

					fakeStackManager.UpdateNodeGroupStackReturns(nil)

					p.MockEKS().On("DescribeNodegroup", &awseks.DescribeNodegroupInput{
						ClusterName:   aws.String(clusterName),
						NodegroupName: aws.String(ngName),
					}).Return(&awseks.DescribeNodegroupOutput{
						Nodegroup: &awseks.Nodegroup{
							NodegroupName:  aws.String(ngName),
							ClusterName:    aws.String(clusterName),
							Status:         aws.String("my-status"),
							AmiType:        aws.String("BOTTLEROCKET_x86_64"),
							Version:        aws.String("1.20"),
							ReleaseVersion: aws.String("1.20-20201212"),
						},
					}, nil)

					p.MockSSM().On("GetParameter", &ssm.GetParameterInput{
						Name: aws.String("/aws/service/bottlerocket/aws-k8s-1.21/x86_64/latest/image_version"),
					}).Return(&ssm.GetParameterOutput{
						Parameter: &ssm.Parameter{
							Value: aws.String("1.5.2-1602f3a8"),
						},
					}, nil)
				})

				It("upgrades the nodegroup updating the stack with the kubernetes version", func() {
					Expect(m.Upgrade(options)).To(Succeed())
					Expect(fakeStackManager.GetManagedNodeGroupTemplateCallCount()).To(Equal(1))
					Expect(fakeStackManager.GetManagedNodeGroupTemplateArgsForCall(0).NodeGroupName).To(Equal(ngName))

					By("upgrading the ForceUpdateEnabled setting first")
					Expect(fakeStackManager.UpdateNodeGroupStackCallCount()).To(Equal(2))
					ng, template, wait := fakeStackManager.UpdateNodeGroupStackArgsForCall(0)
					Expect(ng).To(Equal(ngName))
					Expect(template).To(Equal(brForceFalseTemplate))
					Expect(wait).To(BeTrue())

					By("upgrading the Version next")
					ng, template, wait = fakeStackManager.UpdateNodeGroupStackArgsForCall(1)
					Expect(ng).To(Equal(ngName))
					Expect(template).To(Equal(brFulllyUpdatedTemplate))
					Expect(wait).To(BeTrue())
				})
			})
		})
	})
})
