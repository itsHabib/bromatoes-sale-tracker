variable "ami_name" {
  type = string
}

variable "region" {
  type = string
}

variable "server_version" {
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
    destination = "/tmp/disable-thp"
    source = "disable-thp"
  }

  provisioner "file" {
    destination = "/tmp/configure-cb.sh"
    source = "configure-cb.sh"
  }

  provisioner "file" {
    destination = "/tmp/bromato-sales.json"
    source = "bromato-sales-03-03.json"
  }


  provisioner "shell" {
    inline = [
      "sleep 10",
      // configure journald
      "sudo mv /tmp/journald.conf /etc/systemd/journald.conf",
      "sudo chown root:root /etc/systemd/journald.conf",
      "sudo chmod 755 /etc/systemd/journald.conf",

      "sudo mv /tmp/cfg.env /home/ec2-user/cfg.env",
      "sudo chown ec2-user:ec2-user /home/ec2-user/cfg.env",

      // disable thp
      "sudo mv /tmp/disable-thp /etc/init.d/disable-thp",
      "sudo chmod 755 /etc/init.d/disable-thp",
      "sudo chkconfig --add disable-thp",

      // Set swappiness to 1 to avoid swapping excessively
      "sudo sh -c 'echo \"vm.swappiness = 1\" >> /etc/sysctl.conf'",

      // install couchbase
      "curl https://packages.couchbase.com/releases/${var.server_version}/couchbase-server-enterprise-${var.server_version}-amzn2.x86_64.rpm > /tmp/${var.server_version}.rpm",
      "sudo rpm --install /tmp/${var.server_version}.rpm",
      "rm /tmp/${var.server_version}.rpm",
      "sudo mv /tmp/configure-cb.sh /home/ec2-user/configure-cb.sh",
      "sudo usermod -a -G couchbase ec2-user",

      "sudo mv /tmp/bromato-sales.json /home/ec2-user",
      "sudo mv /tmp/bromato.service /lib/systemd/system/bromato.service",
      "sudo mv /tmp/bromato.gz /home/ec2-user",
      "sudo gunzip /home/ec2-user/bromato.gz",
      "sudo chmod +x /home/ec2-user/bromato",
    ]
  }
}


