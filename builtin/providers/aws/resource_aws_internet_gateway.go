package aws

import (
	"fmt"
	"log"
	"time"

	"github.com/hashicorp/aws-sdk-go/aws"
	"github.com/hashicorp/aws-sdk-go/gen/ec2"
	"github.com/hashicorp/terraform/helper/resource"
	"github.com/hashicorp/terraform/helper/schema"
)

func resourceAwsInternetGateway() *schema.Resource {
	return &schema.Resource{
		Create: resourceAwsInternetGatewayCreate,
		Read:   resourceAwsInternetGatewayRead,
		Update: resourceAwsInternetGatewayUpdate,
		Delete: resourceAwsInternetGatewayDelete,

		Schema: map[string]*schema.Schema{
			"vpc_id": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
			},
			"tags": tagsSchema(),
		},
	}
}

func resourceAwsInternetGatewayCreate(d *schema.ResourceData, meta interface{}) error {
	ec2conn := meta.(*AWSClient).ec2conn

	// Create the gateway
	log.Printf("[DEBUG] Creating internet gateway")
	resp, err := ec2conn.CreateInternetGateway(nil)
	if err != nil {
		return fmt.Errorf("Error creating internet gateway: %s", err)
	}

	// Get the ID and store it
	ig := resp.InternetGateway
	d.SetId(*ig.InternetGatewayID)
	log.Printf("[INFO] InternetGateway ID: %s", d.Id())

	err = setTags(ec2conn, d)
	if err != nil {
		return err
	}

	// Attach the new gateway to the correct vpc
	return resourceAwsInternetGatewayAttach(d, meta)
}

func resourceAwsInternetGatewayRead(d *schema.ResourceData, meta interface{}) error {
	ec2conn := meta.(*AWSClient).ec2conn

	igRaw, _, err := IGStateRefreshFunc(ec2conn, d.Id())()
	if err != nil {
		return err
	}
	if igRaw == nil {
		// Seems we have lost our internet gateway
		d.SetId("")
		return nil
	}

	ig := igRaw.(*ec2.InternetGateway)
	if len(ig.Attachments) == 0 {
		// Gateway exists but not attached to the VPC
		d.Set("vpc_id", "")
	} else {
		d.Set("vpc_id", ig.Attachments[0].VPCID)
	}

	d.Set("tags", tagsToMap(ig.Tags))

	return nil
}

func resourceAwsInternetGatewayUpdate(d *schema.ResourceData, meta interface{}) error {
	if d.HasChange("vpc_id") {
		// If we're already attached, detach it first
		if err := resourceAwsInternetGatewayDetach(d, meta); err != nil {
			return err
		}

		// Attach the gateway to the new vpc
		if err := resourceAwsInternetGatewayAttach(d, meta); err != nil {
			return err
		}
	}

	ec2conn := meta.(*AWSClient).ec2conn

	if err := setTags(ec2conn, d); err != nil {
		return err
	}

	d.SetPartial("tags")

	return nil
}

func resourceAwsInternetGatewayDelete(d *schema.ResourceData, meta interface{}) error {
	ec2conn := meta.(*AWSClient).ec2conn

	// Detach if it is attached
	if err := resourceAwsInternetGatewayDetach(d, meta); err != nil {
		return err
	}

	log.Printf("[INFO] Deleting Internet Gateway: %s", d.Id())

	return resource.Retry(5*time.Minute, func() error {
		err := ec2conn.DeleteInternetGateway(&ec2.DeleteInternetGatewayRequest{
			InternetGatewayID: aws.String(d.Id()),
		})
		if err == nil {
			return nil
		}

		ec2err, ok := err.(aws.APIError)
		if !ok {
			return err
		}

		switch ec2err.Code {
		case "InvalidInternetGatewayID.NotFound":
			return nil
		case "DependencyViolation":
			return err // retry
		}

		return resource.RetryError{Err: err}
	})
}

func resourceAwsInternetGatewayAttach(d *schema.ResourceData, meta interface{}) error {
	ec2conn := meta.(*AWSClient).ec2conn

	if d.Get("vpc_id").(string) == "" {
		log.Printf(
			"[DEBUG] Not attaching Internet Gateway '%s' as no VPC ID is set",
			d.Id())
		return nil
	}

	log.Printf(
		"[INFO] Attaching Internet Gateway '%s' to VPC '%s'",
		d.Id(),
		d.Get("vpc_id").(string))

	err := ec2conn.AttachInternetGateway(&ec2.AttachInternetGatewayRequest{
		InternetGatewayID: aws.String(d.Id()),
		VPCID:             aws.String(d.Get("vpc_id").(string)),
	})
	if err != nil {
		return err
	}

	// A note on the states below: the AWS docs (as of July, 2014) say
	// that the states would be: attached, attaching, detached, detaching,
	// but when running, I noticed that the state is usually "available" when
	// it is attached.

	// Wait for it to be fully attached before continuing
	log.Printf("[DEBUG] Waiting for internet gateway (%s) to attach", d.Id())
	stateConf := &resource.StateChangeConf{
		Pending: []string{"detached", "attaching"},
		Target:  "available",
		Refresh: IGAttachStateRefreshFunc(ec2conn, d.Id(), "available"),
		Timeout: 1 * time.Minute,
	}
	if _, err := stateConf.WaitForState(); err != nil {
		return fmt.Errorf(
			"Error waiting for internet gateway (%s) to attach: %s",
			d.Id(), err)
	}

	return nil
}

func resourceAwsInternetGatewayDetach(d *schema.ResourceData, meta interface{}) error {
	ec2conn := meta.(*AWSClient).ec2conn

	// Get the old VPC ID to detach from
	vpcID, _ := d.GetChange("vpc_id")

	if vpcID.(string) == "" {
		log.Printf(
			"[DEBUG] Not detaching Internet Gateway '%s' as no VPC ID is set",
			d.Id())
		return nil
	}

	log.Printf(
		"[INFO] Detaching Internet Gateway '%s' from VPC '%s'",
		d.Id(),
		vpcID.(string))

	wait := true
	err := ec2conn.DetachInternetGateway(&ec2.DetachInternetGatewayRequest{
		InternetGatewayID: aws.String(d.Id()),
		VPCID:             aws.String(vpcID.(string)),
	})
	if err != nil {
		ec2err, ok := err.(aws.APIError)
		if ok {
			if ec2err.Code == "InvalidInternetGatewayID.NotFound" {
				err = nil
				wait = false
			} else if ec2err.Code == "Gateway.NotAttached" {
				err = nil
				wait = false
			}
		}

		if err != nil {
			return err
		}
	}

	if !wait {
		return nil
	}

	// Wait for it to be fully detached before continuing
	log.Printf("[DEBUG] Waiting for internet gateway (%s) to detach", d.Id())
	stateConf := &resource.StateChangeConf{
		Pending: []string{"attached", "detaching", "available"},
		Target:  "detached",
		Refresh: IGAttachStateRefreshFunc(ec2conn, d.Id(), "detached"),
		Timeout: 1 * time.Minute,
	}
	if _, err := stateConf.WaitForState(); err != nil {
		return fmt.Errorf(
			"Error waiting for internet gateway (%s) to detach: %s",
			d.Id(), err)
	}

	return nil
}

// IGStateRefreshFunc returns a resource.StateRefreshFunc that is used to watch
// an internet gateway.
func IGStateRefreshFunc(ec2conn *ec2.EC2, id string) resource.StateRefreshFunc {
	return func() (interface{}, string, error) {
		resp, err := ec2conn.DescribeInternetGateways(&ec2.DescribeInternetGatewaysRequest{
			InternetGatewayIDs: []string{id},
		})
		if err != nil {
			ec2err, ok := err.(aws.APIError)
			if ok && ec2err.Code == "InvalidInternetGatewayID.NotFound" {
				resp = nil
			} else {
				log.Printf("[ERROR] Error on IGStateRefresh: %s", err)
				return nil, "", err
			}
		}

		if resp == nil {
			// Sometimes AWS just has consistency issues and doesn't see
			// our instance yet. Return an empty state.
			return nil, "", nil
		}

		ig := &resp.InternetGateways[0]
		return ig, "available", nil
	}
}

// IGAttachStateRefreshFunc returns a resource.StateRefreshFunc that is used
// watch the state of an internet gateway's attachment.
func IGAttachStateRefreshFunc(ec2conn *ec2.EC2, id string, expected string) resource.StateRefreshFunc {
	var start time.Time
	return func() (interface{}, string, error) {
		if start.IsZero() {
			start = time.Now()
		}

		resp, err := ec2conn.DescribeInternetGateways(&ec2.DescribeInternetGatewaysRequest{
			InternetGatewayIDs: []string{id},
		})
		if err != nil {
			ec2err, ok := err.(aws.APIError)
			if ok && ec2err.Code == "InvalidInternetGatewayID.NotFound" {
				resp = nil
			} else {
				log.Printf("[ERROR] Error on IGStateRefresh: %s", err)
				return nil, "", err
			}
		}

		if resp == nil {
			// Sometimes AWS just has consistency issues and doesn't see
			// our instance yet. Return an empty state.
			return nil, "", nil
		}

		ig := &resp.InternetGateways[0]

		if time.Now().Sub(start) > 10*time.Second {
			return ig, expected, nil
		}

		if len(ig.Attachments) == 0 {
			// No attachments, we're detached
			return ig, "detached", nil
		}

		return ig, *ig.Attachments[0].State, nil
	}
}
