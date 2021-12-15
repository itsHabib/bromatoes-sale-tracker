variable "ami_name" {
  type = string
}

variable "region" {
  type = string
}

source "amazon-ebs" "cc" {
  ami_name      = "${var.ami_name}"
  instance_type = "t2.micro"
  region        = "${var.region}"
  source_ami_filter {
    filters = {
      name                = "amzn2-ami-hvm-2.0.*-x86_64-gp2"
      root-device-type    = "ebs"
      virtualization-type = "hvm"
    }
    most_recent = true
    owners      = ["amazon"]
  }
  ssh_username = "ec2-user"
}

# a build block invokes sources and runs provisioning steps on them.
build {
  sources = ["source.amazon-ebs.cc"]

  provisioner "file" {
    destination = "/tmp/"
    source      = "bromato.gz"
  }

  provisioner "file" {
    destination = "/tmp/"
    source      = "bromato.service"
  }

  provisioner "file" {
    destination = "/tmp/journald.conf"
    source = "journald.conf"
  }

  provisioner "file" {
    destination = "/tmp/cfg.env"
    source = "cfg.env"
  }

  provisioner "file" {
    destination = "/tmp/trialcert.cer"
    source = "trialcert.cer"
  }

  provisioner "shell" {
    inline = [
      "sleep 10",
      "sudo mv /tmp/journald.conf /etc/systemd/journald.conf",
      "sudo chown root:root /etc/systemd/journald.conf",
      "sudo chmod 755 /etc/systemd/journald.conf",

      "sudo mv /tmp/cfg.env /home/ec2-user/cfg.env",
      "sudo chown ec2-user:ec2-user /home/ec2-user/cfg.env",

      "sudo mv /tmp/trialcert.cer /home/ec2-user/trialcert.cer",
      "sudo chown ec2-user:ec2-user /home/ec2-user/trialcert.cer",

      "sudo mv /tmp/bromato.service /lib/systemd/system/bromato.service",
      "sudo mv /tmp/bromato.gz /home/ec2-user",
      "sudo gunzip /home/ec2-user/bromato.gz",
      "sudo chmod +x /home/ec2-user/bromato",
      "sudo systemctl enable bromato.service",
    ]
  }
}


