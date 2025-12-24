extends Node

const packets := preload("res://packets.gd")

func _ready() -> void:
	var packet := packets.Packet.new()
	var chat_msg := packet.new_chat()
	
	packet.set_sender_id(69)
	chat_msg.set_msg("Hello, world!")
	
	var data := packet.to_bytes()
	
	print(data)
