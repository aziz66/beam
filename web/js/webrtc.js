const rtcConfig = {
  iceServers: [
    { urls: 'stun:stun.l.google.com:19302' },
    { urls: 'stun:stun1.l.google.com:19302' }
  ]
}

export class WebRTCManager {
  constructor(transport) {
    this.transport = transport
    this.pc = null
    this.dataChannel = null
    this.active = false
    this.myDeviceId = null
    this.peerDeviceId = null
    this.onMessage = null
    this.onStateChange = null

    transport.on('signal_offer', (env) => this._handleOffer(env))
    transport.on('signal_answer', (env) => this._handleAnswer(env))
    transport.on('signal_ice', (env) => this._handleICE(env))
  }

  // Attempt P2P only for 2-device rooms
  tryConnect(devices, myDeviceId) {
    this.myDeviceId = myDeviceId

    if (devices.length !== 2 || this.active || this.pc) return

    const peer = devices.find(d => d.device_id !== myDeviceId)
    if (!peer) return
    this.peerDeviceId = peer.device_id

    // Deterministic initiator: device with smaller ID initiates
    if (myDeviceId < peer.device_id) {
      this._initiateConnection()
    }
  }

  _initiateConnection() {
    this._createPeerConnection()

    this.dataChannel = this.pc.createDataChannel('beam', {
      ordered: true
    })
    this._setupDataChannel(this.dataChannel)

    this.pc.createOffer()
      .then(offer => this.pc.setLocalDescription(offer))
      .then(() => {
        this.transport.send({
          type: 'signal_offer',
          payload: { sdp: this.pc.localDescription.sdp },
          target_id: this.peerDeviceId,
          ts: Date.now()
        })
      })
      .catch(err => console.error('webrtc offer failed:', err))
  }

  _createPeerConnection() {
    this.pc = new RTCPeerConnection(rtcConfig)

    this.pc.onicecandidate = (event) => {
      if (event.candidate && this.peerDeviceId) {
        this.transport.send({
          type: 'signal_ice',
          payload: { candidate: JSON.stringify(event.candidate) },
          target_id: this.peerDeviceId,
          ts: Date.now()
        })
      }
    }

    this.pc.onconnectionstatechange = () => {
      const state = this.pc.connectionState
      if (state === 'connected') {
        this.active = true
        if (this.onStateChange) this.onStateChange('connected')
      } else if (state === 'disconnected' || state === 'failed' || state === 'closed') {
        this.active = false
        if (this.onStateChange) this.onStateChange('disconnected')
        this._cleanup()
      }
    }

    this.pc.ondatachannel = (event) => {
      this._setupDataChannel(event.channel)
    }
  }

  _setupDataChannel(channel) {
    this.dataChannel = channel

    channel.onopen = () => {
      this.active = true
      if (this.onStateChange) this.onStateChange('connected')
    }

    channel.onclose = () => {
      this.active = false
      if (this.onStateChange) this.onStateChange('disconnected')
    }

    channel.onmessage = (event) => {
      if (this.onMessage) {
        try {
          const env = JSON.parse(event.data)
          this.onMessage(env)
        } catch {}
      }
    }
  }

  async _handleOffer(env) {
    const payload = env.payload
    this.peerDeviceId = env.device_id

    this._createPeerConnection()

    await this.pc.setRemoteDescription({
      type: 'offer',
      sdp: payload.sdp
    })

    const answer = await this.pc.createAnswer()
    await this.pc.setLocalDescription(answer)

    this.transport.send({
      type: 'signal_answer',
      payload: { sdp: this.pc.localDescription.sdp },
      target_id: this.peerDeviceId,
      ts: Date.now()
    })
  }

  async _handleAnswer(env) {
    if (!this.pc) return
    await this.pc.setRemoteDescription({
      type: 'answer',
      sdp: env.payload.sdp
    })
  }

  async _handleICE(env) {
    if (!this.pc) return
    try {
      const candidate = JSON.parse(env.payload.candidate)
      await this.pc.addIceCandidate(candidate)
    } catch (err) {
      console.error('webrtc ice failed:', err)
    }
  }

  send(envelope) {
    if (this.active && this.dataChannel && this.dataChannel.readyState === 'open') {
      this.dataChannel.send(JSON.stringify(envelope))
      return true
    }
    return false
  }

  close() {
    this._cleanup()
  }

  _cleanup() {
    if (this.dataChannel) {
      this.dataChannel.close()
      this.dataChannel = null
    }
    if (this.pc) {
      this.pc.close()
      this.pc = null
    }
    this.active = false
    this.peerDeviceId = null
  }
}
