package aws

import (
	"fmt"
	"os"
	"reflect"
	"sort"
	"testing"

	"github.com/hashicorp/aws-sdk-go/aws"
	"github.com/hashicorp/aws-sdk-go/gen/elb"
	"github.com/hashicorp/terraform/helper/resource"
	"github.com/hashicorp/terraform/terraform"
)

func TestAccAWSELB_basic(t *testing.T) {
	var conf elb.LoadBalancerDescription
	ssl_certificate_id := os.Getenv("AWS_SSL_CERTIFICATE_ID")

	resource.Test(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckAWSELBDestroy,
		Steps: []resource.TestStep{
			resource.TestStep{
				Config: testAccAWSELBConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSELBExists("aws_elb.bar", &conf),
					testAccCheckAWSELBAttributes(&conf),
					resource.TestCheckResourceAttr(
						"aws_elb.bar", "name", "foobar-terraform-test"),
					resource.TestCheckResourceAttr(
						"aws_elb.bar", "availability_zones.2487133097", "us-west-2a"),
					resource.TestCheckResourceAttr(
						"aws_elb.bar", "availability_zones.221770259", "us-west-2b"),
					resource.TestCheckResourceAttr(
						"aws_elb.bar", "availability_zones.2050015877", "us-west-2c"),
					resource.TestCheckResourceAttr(
						"aws_elb.bar", "listener.206423021.instance_port", "8000"),
					resource.TestCheckResourceAttr(
						"aws_elb.bar", "listener.206423021.instance_protocol", "http"),
					resource.TestCheckResourceAttr(
						"aws_elb.bar", "listener.206423021.ssl_certificate_id", ssl_certificate_id),
					resource.TestCheckResourceAttr(
						"aws_elb.bar", "listener.206423021.lb_port", "80"),
					resource.TestCheckResourceAttr(
						"aws_elb.bar", "listener.206423021.lb_protocol", "http"),
					resource.TestCheckResourceAttr(
						"aws_elb.bar", "cross_zone_load_balancing", "true"),
				),
			},
		},
	})
}

func TestAccAWSELB_InstanceAttaching(t *testing.T) {
	var conf elb.LoadBalancerDescription

	testCheckInstanceAttached := func(count int) resource.TestCheckFunc {
		return func(*terraform.State) error {
			if len(conf.Instances) != count {
				return fmt.Errorf("instance count does not match")
			}
			return nil
		}
	}

	resource.Test(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckAWSELBDestroy,
		Steps: []resource.TestStep{
			resource.TestStep{
				Config: testAccAWSELBConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSELBExists("aws_elb.bar", &conf),
					testAccCheckAWSELBAttributes(&conf),
				),
			},

			resource.TestStep{
				Config: testAccAWSELBConfigNewInstance,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSELBExists("aws_elb.bar", &conf),
					testCheckInstanceAttached(1),
				),
			},
		},
	})
}

func TestAccAWSELB_HealthCheck(t *testing.T) {
	var conf elb.LoadBalancerDescription

	resource.Test(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckAWSELBDestroy,
		Steps: []resource.TestStep{
			resource.TestStep{
				Config: testAccAWSELBConfigHealthCheck,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSELBExists("aws_elb.bar", &conf),
					testAccCheckAWSELBAttributesHealthCheck(&conf),
					resource.TestCheckResourceAttr(
						"aws_elb.bar", "health_check.3484319807.healthy_threshold", "5"),
					resource.TestCheckResourceAttr(
						"aws_elb.bar", "health_check.3484319807.unhealthy_threshold", "5"),
					resource.TestCheckResourceAttr(
						"aws_elb.bar", "health_check.3484319807.target", "HTTP:8000/"),
					resource.TestCheckResourceAttr(
						"aws_elb.bar", "health_check.3484319807.timeout", "30"),
					resource.TestCheckResourceAttr(
						"aws_elb.bar", "health_check.3484319807.interval", "60"),
				),
			},
		},
	})
}

func TestAccAWSELBUpdate_HealthCheck(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckAWSELBDestroy,
		Steps: []resource.TestStep{
			resource.TestStep{
				Config: testAccAWSELBConfigHealthCheck,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"aws_elb.bar", "health_check.3484319807.healthy_threshold", "5"),
				),
			},
			resource.TestStep{
				Config: testAccAWSELBConfigHealthCheck_update,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"aws_elb.bar", "health_check.2648756019.healthy_threshold", "10"),
				),
			},
		},
	})
}

func testAccCheckAWSELBDestroy(s *terraform.State) error {
	conn := testAccProvider.Meta().(*AWSClient).elbconn

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "aws_elb" {
			continue
		}

		describe, err := conn.DescribeLoadBalancers(&elb.DescribeAccessPointsInput{
			LoadBalancerNames: []string{rs.Primary.ID},
		})

		if err == nil {
			if len(describe.LoadBalancerDescriptions) != 0 &&
				*describe.LoadBalancerDescriptions[0].LoadBalancerName == rs.Primary.ID {
				return fmt.Errorf("ELB still exists")
			}
		}

		// Verify the error
		providerErr, ok := err.(aws.APIError)
		if !ok {
			return err
		}

		if providerErr.Code != "InvalidLoadBalancerName.NotFound" {
			return fmt.Errorf("Unexpected error: %s", err)
		}
	}

	return nil
}

func testAccCheckAWSELBAttributes(conf *elb.LoadBalancerDescription) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		zones := []string{"us-west-2a", "us-west-2b", "us-west-2c"}
		sort.StringSlice(conf.AvailabilityZones).Sort()
		if !reflect.DeepEqual(conf.AvailabilityZones, zones) {
			return fmt.Errorf("bad availability_zones")
		}

		if *conf.LoadBalancerName != "foobar-terraform-test" {
			return fmt.Errorf("bad name")
		}

		l := elb.Listener{
			InstancePort:     aws.Integer(8000),
			InstanceProtocol: aws.String("HTTP"),
			LoadBalancerPort: aws.Integer(80),
			Protocol:         aws.String("HTTP"),
		}

		if !reflect.DeepEqual(conf.ListenerDescriptions[0].Listener, &l) {
			return fmt.Errorf(
				"Got:\n\n%#v\n\nExpected:\n\n%#v\n",
				conf.ListenerDescriptions[0].Listener,
				l)
		}

		if *conf.DNSName == "" {
			return fmt.Errorf("empty dns_name")
		}

		return nil
	}
}

func testAccCheckAWSELBAttributesHealthCheck(conf *elb.LoadBalancerDescription) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		zones := []string{"us-west-2a", "us-west-2b", "us-west-2c"}
		sort.StringSlice(conf.AvailabilityZones).Sort()
		if !reflect.DeepEqual(conf.AvailabilityZones, zones) {
			return fmt.Errorf("bad availability_zones")
		}

		if *conf.LoadBalancerName != "foobar-terraform-test" {
			return fmt.Errorf("bad name")
		}

		check := elb.HealthCheck{
			Timeout:            aws.Integer(30),
			UnhealthyThreshold: aws.Integer(5),
			HealthyThreshold:   aws.Integer(5),
			Interval:           aws.Integer(60),
			Target:             aws.String("HTTP:8000/"),
		}

		if !reflect.DeepEqual(conf.HealthCheck, &check) {
			return fmt.Errorf(
				"Got:\n\n%#v\n\nExpected:\n\n%#v\n",
				conf.HealthCheck,
				check)
		}

		if *conf.DNSName == "" {
			return fmt.Errorf("empty dns_name")
		}

		return nil
	}
}

func testAccCheckAWSELBExists(n string, res *elb.LoadBalancerDescription) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[n]
		if !ok {
			return fmt.Errorf("Not found: %s", n)
		}

		if rs.Primary.ID == "" {
			return fmt.Errorf("No ELB ID is set")
		}

		conn := testAccProvider.Meta().(*AWSClient).elbconn

		describe, err := conn.DescribeLoadBalancers(&elb.DescribeAccessPointsInput{
			LoadBalancerNames: []string{rs.Primary.ID},
		})

		if err != nil {
			return err
		}

		if len(describe.LoadBalancerDescriptions) != 1 ||
			*describe.LoadBalancerDescriptions[0].LoadBalancerName != rs.Primary.ID {
			return fmt.Errorf("ELB not found")
		}

		*res = describe.LoadBalancerDescriptions[0]

		return nil
	}
}

const testAccAWSELBConfig = `
resource "aws_elb" "bar" {
  name = "foobar-terraform-test"
  availability_zones = ["us-west-2a", "us-west-2b", "us-west-2c"]

  listener {
    instance_port = 8000
    instance_protocol = "http"
    lb_port = 80
    lb_protocol = "http"
  }

  cross_zone_load_balancing = true
}
`

const testAccAWSELBConfigNewInstance = `
resource "aws_elb" "bar" {
  name = "foobar-terraform-test"
  availability_zones = ["us-west-2a", "us-west-2b", "us-west-2c"]

  listener {
    instance_port = 8000
    instance_protocol = "http"
    lb_port = 80
    lb_protocol = "http"
  }

  instances = ["${aws_instance.foo.id}"]
}

resource "aws_instance" "foo" {
	# us-west-2
	ami = "ami-043a5034"
	instance_type = "t1.micro"
}
`

const testAccAWSELBConfigListenerSSLCertificateId = `
resource "aws_elb" "bar" {
  name = "foobar-terraform-test"
  availability_zones = ["us-west-2a"]

  listener {
    instance_port = 8000
    instance_protocol = "http"
    ssl_certificate_id = "%s"
    lb_port = 443
    lb_protocol = "https"
  }
}
`

const testAccAWSELBConfigHealthCheck = `
resource "aws_elb" "bar" {
  name = "foobar-terraform-test"
  availability_zones = ["us-west-2a", "us-west-2b", "us-west-2c"]

  listener {
    instance_port = 8000
    instance_protocol = "http"
    lb_port = 80
    lb_protocol = "http"
  }

  health_check {
    healthy_threshold = 5
    unhealthy_threshold = 5
    target = "HTTP:8000/"
    interval = 60
    timeout = 30
  }
}
`

const testAccAWSELBConfigHealthCheck_update = `
resource "aws_elb" "bar" {
  name = "foobar-terraform-test"
  availability_zones = ["us-west-2a"]

  listener {
    instance_port = 8000
    instance_protocol = "http"
    lb_port = 80
    lb_protocol = "http"
  }

  health_check {
    healthy_threshold = 10
    unhealthy_threshold = 5
    target = "HTTP:8000/"
    interval = 60
    timeout = 30
  }
}
`
