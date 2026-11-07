// Runs on the audio thread. Collects microphone samples into frames of a
// fixed size and posts each full frame to the main thread, with the voice
// probability when RNNoise runs. Mono, or stereo for music mode: then a
// frame is both channels one after the other (f32-planar). The frames (the
// first channel) also go to the output, one frame late: the settings' echo
// test plays them (else its gain is 0).

const RNNOISE_FRAME = 480; // 10 ms at 48 kHz
const SCALE = 32768; // RNNoise works on 16-bit sample values

// createDenoiser runs RNNoise (the Jitsi build of Xiph's RNNoise). The
// main thread fetches it (worklets cannot) and posts the bytes: Chrome does
// not post compiled modules to worklets. Its only imports grow and copy
// memory. process() filters a 480-sample frame in place and returns the
// voice probability (0 to 1).
export function createDenoiser(module) {
  let memory;
  const instance = new WebAssembly.Instance(module, {
    a: {
      a: (size) => {
        try {
          memory.grow(Math.ceil((size - memory.buffer.byteLength) / 65536));
          return 1;
        } catch {
          return 0;
        }
      },
      b: (dest, src, n) => new Uint8Array(memory.buffer).copyWithin(dest, src, src + n),
    },
  });
  // Export names of this build: c memory, d constructors, f create, g
  // malloc, h destroy, i free, j process_frame.
  const x = instance.exports;
  memory = x.c;
  x.d();
  const state = x.f(0);
  const buf = x.g(RNNOISE_FRAME * 4);
  return {
    process(frame) {
      if (frame.length !== RNNOISE_FRAME) throw new Error(`RNNoise needs ${RNNOISE_FRAME} samples`);
      const heap = new Float32Array(memory.buffer, buf, RNNOISE_FRAME); // new view: memory may grow
      for (let i = 0; i < RNNOISE_FRAME; i++) heap[i] = frame[i] * SCALE;
      const vad = x.j(state, buf, buf);
      const out = new Float32Array(memory.buffer, buf, RNNOISE_FRAME);
      for (let i = 0; i < RNNOISE_FRAME; i++) frame[i] = out[i] / SCALE;
      return vad;
    },
    destroy() {
      x.i(buf);
      x.h(state);
    },
  };
}

// Fifo is a ring buffer of samples; when full, the oldest go.
export class Fifo {
  constructor(size) {
    this.buf = new Float32Array(size);
    this.start = 0;
    this.length = 0;
  }
  push(samples) {
    for (const s of samples) {
      this.buf[(this.start + this.length) % this.buf.length] = s;
      if (this.length < this.buf.length) this.length++;
      else this.start = (this.start + 1) % this.buf.length;
    }
  }
  // Fills out; silence where there is nothing.
  pull(out) {
    for (let i = 0; i < out.length; i++) {
      if (this.length === 0) {
        out[i] = 0;
        continue;
      }
      out[i] = this.buf[this.start];
      this.start = (this.start + 1) % this.buf.length;
      this.length--;
    }
  }
}

// Input channel ch (the first when there are fewer), or silence when the
// input has none: an inactive input (Firefox's for a silent source) has no
// channels, and without frames a talk spurt would never end.
let silence = new Float32Array(0); // reused: no garbage per block
export function inputBlock(inputs, quantum, ch = 0) {
  const block = inputs[0]?.[ch] ?? inputs[0]?.[0];
  if (block) return block;
  if (silence.length !== quantum) silence = new Float32Array(quantum);
  return silence;
}

if (typeof AudioWorkletProcessor !== "undefined") {
  class CaptureProcessor extends AudioWorkletProcessor {
    constructor(options) {
      super();
      this.size = options.processorOptions.frameSize;
      this.channels = 1;
      this.frame = new Float32Array(this.size);
      this.filled = 0;
      this.denoiser = null;
      this.echo = new Fifo(this.size * 5);
      // { denoise: ArrayBuffer | null }: RNNoise (its bytes) on or off. It
      // is made once and kept until closed (switched off, not destroyed). The answer
      // says whether it runs ({ denoising }) or failed. { channels }: 1 or
      // 2, from a new frame on.
      this.denoising = false;
      this.active = false;
      this.stopped = false;
      this.port.onmessage = ({ data }) => {
        if (data.stop) {
          this.stopped = true;
          this.denoiser?.destroy();
          this.denoiser = null;
          this.port.close();
          return;
        }
        if ("active" in data) {
          this.active = data.active;
          this.filled = 0;
          this.echo.start = this.echo.length = 0;
        }
        if ("channels" in data) {
          this.channels = data.channels;
          this.frame = new Float32Array(this.size * this.channels);
          this.filled = 0;
        }
        if (!("denoise" in data)) return;
        try {
          if (data.denoise && !this.denoiser) this.denoiser = createDenoiser(new WebAssembly.Module(data.denoise));
          this.denoising = !!data.denoise;
          this.port.postMessage({ denoising: this.denoising });
        } catch (err) {
          this.port.postMessage({ denoiseError: String(err) });
        }
      };
      this.port.onmessageerror = () => this.port.postMessage({ denoiseError: "the message did not arrive" });
    }

    process(inputs, outputs) {
      if (this.stopped) return false;
      if (!this.active) return true;
      const out = outputs[0]?.[0];
      const quantum = out?.length ?? 128;
      const left = inputBlock(inputs, quantum);
      const right = this.channels === 2 ? inputBlock(inputs, quantum, 1) : null;
      for (let i = 0; i < left.length; i++) {
        if (right) this.frame[this.size + this.filled] = right[i];
        this.frame[this.filled++] = left[i];
        if (this.filled === this.size) this.flush();
      }
      if (out) this.echo.pull(out);
      return true;
    }

    flush() {
      let vad;
      if (this.denoising) {
        vad = 0;
        for (let at = 0; at + RNNOISE_FRAME <= this.size; at += RNNOISE_FRAME)
          vad = Math.max(vad, this.denoiser.process(this.frame.subarray(at, at + RNNOISE_FRAME)));
      }
      this.echo.push(this.frame.subarray(0, this.size));
      this.port.postMessage({ samples: this.frame, vad }, [this.frame.buffer]);
      this.frame = new Float32Array(this.size * this.channels);
      this.filled = 0;
    }
  }
  registerProcessor("capture", CaptureProcessor);
}
