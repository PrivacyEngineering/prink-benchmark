output "client_ip" {
  value = aws_instance.client.public_ip
}

output "server_ip" {
  value = aws_instance.server.public_ip
}