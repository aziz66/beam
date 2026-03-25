// ICE / STUN configuration.
// Privacy note: during ICE candidate gathering the browser contacts each STUN
// server, which means those servers (Google, Cloudflare, stunprotocol.org) see
// the client's public IP address. This is inherent to WebRTC NAT traversal.
// Self-hosters who want to avoid leaking IPs to third parties should replace
// these with a STUN/TURN server they control.
const rtcConfig = {
  iceServers: [
    { urls: 'stun:stun.l.google.com:19302' },
    { urls: 'stun:stun1.l.google.com:19302' },
    { urls: 'stun:stun.cloudflare.com:3478' },
    { urls: 'stun:stun.stunprotocol.org:3478' }
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
    // ICE candidates that arrived before setRemoteDescription completed
    this._pendingCandidates = []

    transport.on('signal_offer', (env) => this._handleOffer(env))
    transport.on('signal_answer', (env) => this._handleAnswer(env))
    transport.on('signal_ice', (env) => this._handleICE(env))
  }

  // Attempt P2P only for 2-device rooms
  tryConnect(devices, myDeviceId) {
    if (!myDeviceId) return // not yet joined — myDeviceId is null until server sends 'joined'
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
    // Capture pc and peerDeviceId now — _cleanup() may null them during await
    const pc = this.pc
    const peerDeviceId = this.peerDeviceId

    this.dataChannel = pc.createDataChannel('beam', { ordered: true })
    this._setupDataChannel(this.dataChannel)

    pc.createOffer()
      .then(offer => pc.setLocalDescription(offer))
      .then(() => {
        if (!pc.localDescription) return // cleanup fired during await
        this.transport.send({
          type: 'signal_offer',
          payload: { sdp: pc.localDescription.sdp },
          target_id: peerDeviceId,
          ts: Date.now()
        })
      })
      .catch(err => console.error('webrtc offer failed:', err))
  }

  _createPeerConnection() {
    // Close any existing connection before creating a new one
    if (this.pc) {
      this.pc.onicecandidate = null
      this.pc.onconnectionstatechange = null
      this.pc.ondatachannel = null
      this.pc.close()
      this.pc = null
    }
    this.pc = new RTCPeerConnection(rtcConfig)
    // Reset ICE candidate buffer for fresh peer connection
    this._pendingCandidates = []

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

    // Reject offers from devices other than the known peer — prevents a third
    // device in the room from hijacking or disrupting an established P2P session.
    if (this.peerDeviceId && env.device_id !== this.peerDeviceId) {
      console.warn('webrtc: ignoring offer from unexpected peer', env.device_id)
      return
    }
    this.peerDeviceId = env.device_id

    this._createPeerConnection()

    try {
      await this.pc.setRemoteDescription({ type: 'offer', sdp: payload.sdp })

      // Flush ICE candidates that arrived before remote description was ready
      for (const candidate of this._pendingCandidates) {
        try { await this.pc.addIceCandidate(candidate) } catch (err) { console.warn('webrtc: addIceCandidate failed:', err) }
      }
      this._pendingCandidates = []

      const answer = await this.pc.createAnswer()
      await this.pc.setLocalDescription(answer)

      this.transport.send({
        type: 'signal_answer',
        payload: { sdp: this.pc.localDescription.sdp },
        target_id: this.peerDeviceId,
        ts: Date.now()
      })
    } catch (err) {
      console.error('webrtc answer failed:', err)
      this._cleanup()
    }
  }

  async _handleAnswer(env) {
    if (!this.pc) return
    // Reject answers from unexpected peers — prevents a third device from
    // injecting a rogue SDP answer and hijacking the P2P session.
    if (this.peerDeviceId && env.device_id !== this.peerDeviceId) {
      console.warn('webrtc: ignoring answer from unexpected peer', env.device_id)
      return
    }
    try {
      await this.pc.setRemoteDescription({ type: 'answer', sdp: env.payload.sdp })

      // Flush ICE candidates that arrived before remote description was ready
      for (const candidate of this._pendingCandidates) {
        try { await this.pc.addIceCandidate(candidate) } catch (err) { console.warn('webrtc: addIceCandidate failed:', err) }
      }
      this._pendingCandidates = []
    } catch (err) {
      console.error('webrtc set answer failed:', err)
      this._cleanup()
    }
  }

  async _handleICE(env) {
    // Capture pc before any await — _cleanup() may null this.pc while we're
    // awaiting addIceCandidate, causing an uncaught rejection on a closed connection.
    const pc = this.pc
    if (!pc) return
    try {
      const candidate = JSON.parse(env.payload.candidate)
      // Buffer candidates until remote description is set — adding them too early
      // throws InvalidStateError and permanently loses the candidate.
      if (!pc.remoteDescription) {
        this._pendingCandidates.push(candidate)
        return
      }
      await pc.addIceCandidate(candidate)
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
      this.dataChannel.onopen = null
      this.dataChannel.onclose = null
      this.dataChannel.onmessage = null
      this.dataChannel.close()
      this.dataChannel = null
    }
    if (this.pc) {
      this.pc.onicecandidate = null
      this.pc.onconnectionstatechange = null
      this.pc.ondatachannel = null
      this.pc.close()
      this.pc = null
    }
    this.active = false
    this.peerDeviceId = null
    this._pendingCandidates = []
  }
}
