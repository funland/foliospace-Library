type SpeechSynthesisLike = Pick<
  SpeechSynthesis,
  "addEventListener" | "cancel" | "getVoices" | "pause" | "removeEventListener" | "resume" | "speak"
>;

type UtteranceFactory = (text: string) => SpeechSynthesisUtterance;

export type BrowserTtsVoice = {
  displayName: string;
  gender: "female" | "male" | "unknown";
  id: string;
  isDefault: boolean;
  locale: string;
};

export type BrowserTtsSpeakOptions = {
  initialMarkerFallbackMs?: number;
  onBoundary?: (event: SpeechSynthesisEvent) => void;
  onEnd?: () => void;
  onError?: (error: SpeechSynthesisErrorEvent | Event) => void;
  onStart?: () => void;
  rate: number;
  voiceId: string;
  volume: number;
};

type BrowserTtsClientDeps = {
  speechSynthesis?: SpeechSynthesisLike;
  utteranceFactory?: UtteranceFactory;
};

export type EpubTtsTextPart = {
  omit?: boolean;
  sourceOffset?: number;
  text: string;
};

export type EpubTtsTextPosition = {
  node?: Text;
  nodeOffset?: number;
  normalizedOffset: number;
  sourceOffset: number;
};

export type EpubTtsTextResult = {
  positions: EpubTtsTextPosition[];
  text: string;
};

export type EpubTtsBlock = {
  blockId: string;
  locatorText: string;
  sourceEnd: number;
  sourceStart: number;
  spineItemId: string;
  text: string;
};

export type EpubTtsMarker = {
  blockId?: string;
  end: number;
  locatorText?: string;
  sourceEnd?: number;
  sourceStart?: number;
  spineItemId?: string;
  start: number;
  text: string;
};

export type EpubTtsChunk = {
  markers: EpubTtsMarker[];
  pauseAfterMs?: number;
  text: string;
};

export type EpubTtsQueueState = {
  chunkIndex: number;
  currentText: string;
  markerBlockId: string;
  markerEndOffset: number;
  markerIndex: number;
  markerLocatorText: string;
  markerStartOffset: number;
  markerText: string;
  spineItemId: string;
  status: "idle" | "loading" | "playing" | "paused" | "error";
};

export type EpubTtsActiveSegment = {
  blockId?: string;
  endOffset?: number;
  locatorText?: string;
  spineItemId: string;
  startOffset?: number;
  text: string;
};

type EpubTtsQueueClient = {
  pause(): void;
  resume(): void;
  speakSelection(text: string, options: BrowserTtsSpeakOptions): Promise<void>;
  stop(): void;
};

type EpubTtsQueueDeps = {
  client: EpubTtsQueueClient;
  onStateChange?: (state: EpubTtsQueueState) => void;
};

type EpubTtsQueueStartArgs = {
  chunks: Array<EpubTtsChunk | string>;
  request: Omit<BrowserTtsSpeakOptions, "onEnd" | "onError">;
};

type ExtractEpubTtsBlocksOptions = {
  pagePosition?: number;
  pageWidth?: number;
};

type EpubTtsChunkOptions = {
  firstSegmentMax?: number;
  preserveBlockBoundaries?: boolean;
  segmentMax?: number;
};

const ttsBlockSelector = "p, li, blockquote, h1, h2, h3, h4, h5, h6";
const ttsBlockIDAttribute = "data-foliospace-tts-block-id";
const ttsActiveClass = "reader-tts-active-segment";
const initialMarkerFallbackMs = 250;
const activeTtsElements = new WeakMap<Document, Element>();

function inferGender(voice: SpeechSynthesisVoice): BrowserTtsVoice["gender"] {
  const name = voice.name.toLowerCase();
  if (/(ava|aria|bella|jenny|zira|female|woman|girl)/.test(name)) return "female";
  if (/(andrew|adam|david|guy|mark|michael|male|man|boy)/.test(name)) return "male";
  return "unknown";
}

function voiceRank(voice: SpeechSynthesisVoice) {
  const name = voice.name.toLowerCase();
  const locale = voice.lang.toLowerCase();
  return [locale.startsWith("en") ? 0 : 1, name.includes("natural") ? 0 : 1, voice.default ? 0 : 1] as const;
}

function normalizeVoice(voice: SpeechSynthesisVoice): BrowserTtsVoice {
  return {
    displayName: voice.name,
    gender: inferGender(voice),
    id: voice.voiceURI || voice.name,
    isDefault: voice.default,
    locale: voice.lang,
  };
}

async function waitForVoices(speechSynthesis: SpeechSynthesisLike): Promise<SpeechSynthesisVoice[]> {
  const immediate = speechSynthesis.getVoices();
  if (immediate.length) return immediate;

  return new Promise<SpeechSynthesisVoice[]>((resolve) => {
    const handleVoicesChanged = () => {
      speechSynthesis.removeEventListener("voiceschanged", handleVoicesChanged);
      resolve(speechSynthesis.getVoices());
    };
    speechSynthesis.addEventListener("voiceschanged", handleVoicesChanged);
  });
}

export function createBrowserTtsClient({
  speechSynthesis = globalThis.speechSynthesis as SpeechSynthesisLike | undefined,
  utteranceFactory = (text) => new SpeechSynthesisUtterance(text),
}: BrowserTtsClientDeps = {}) {
  async function getSpeechVoices() {
    if (!speechSynthesis) throw new Error("speechSynthesis unavailable");
    return waitForVoices(speechSynthesis);
  }

  return {
    async getVoices(): Promise<BrowserTtsVoice[]> {
      const voices = await getSpeechVoices();
      return [...voices]
        .sort((left, right) => {
          const leftRank = voiceRank(left);
          const rightRank = voiceRank(right);
          return leftRank < rightRank ? -1 : leftRank > rightRank ? 1 : 0;
        })
        .filter((voice) => voice.lang.toLowerCase().startsWith("en"))
        .map(normalizeVoice);
    },
    pause() {
      speechSynthesis?.pause();
    },
    resume() {
      speechSynthesis?.resume();
    },
    async speakSelection(text: string, options: BrowserTtsSpeakOptions) {
      if (!speechSynthesis) throw new Error("speechSynthesis unavailable");
      const voices = await getSpeechVoices();
      const utterance = utteranceFactory(text);
      const voice = voices.find((item) => (item.voiceURI || item.name) === options.voiceId) ?? voices[0] ?? null;

      utterance.onstart = () => options.onStart?.();
      utterance.onend = () => options.onEnd?.();
      utterance.onboundary = (event) => options.onBoundary?.(event);
      utterance.onerror = (event) => options.onError?.(event);
      utterance.rate = options.rate;
      utterance.volume = options.volume;
      utterance.voice = voice;
      speechSynthesis.cancel();
      speechSynthesis.speak(utterance);
    },
    stop() {
      speechSynthesis?.cancel();
    },
  };
}

export function normalizeEpubTtsText(parts: EpubTtsTextPart[]): EpubTtsTextResult {
  let text = "";
  let sourceCursor = 0;
  let pendingSpace = false;
  const positions: EpubTtsTextPosition[] = [];

  const appendSpace = () => {
    if (!text || text.endsWith(" ")) return;
    text += " ";
    pendingSpace = false;
  };

  for (const part of parts) {
    if (part.omit) continue;
    const sourceBase = part.sourceOffset ?? sourceCursor;
    for (let index = 0; index < part.text.length; index += 1) {
      const character = part.text[index];
      if (/\s/.test(character)) {
        pendingSpace = true;
        continue;
      }
      if (pendingSpace) appendSpace();
      positions.push({
        normalizedOffset: text.length,
        sourceOffset: sourceBase + index,
      });
      text += character;
      pendingSpace = false;
    }
    sourceCursor += part.text.length;
    pendingSpace = true;
  }

  return {
    positions,
    text: text.trim(),
  };
}

export function extractEpubTtsBlocks(
  doc: Document,
  spineItemId: string,
  options: ExtractEpubTtsBlocksOptions = {},
): EpubTtsBlock[] {
  const pageStart = typeof options.pagePosition === "number" && typeof options.pageWidth === "number"
    ? Math.max(0, options.pagePosition * options.pageWidth - 2)
    : 0;
  const blocks = Array.from(doc.querySelectorAll<HTMLElement>(ttsBlockSelector));

  return blocks
    .filter((element) => {
      if (!pageStart) return true;
      const offsetLeft = Number((element as { offsetLeft?: number }).offsetLeft);
      return !Number.isFinite(offsetLeft) || offsetLeft >= pageStart;
    })
    .map((element, index) => {
      const result = collectElementTtsText(element);
      if (!result.text) return null;
      const blockId = ensureTtsBlockID(element, index);
      return {
        blockId,
        locatorText: result.text,
        sourceEnd: result.text.length,
        sourceStart: 0,
        spineItemId,
        text: result.text,
      };
    })
    .filter((block): block is EpubTtsBlock => Boolean(block));
}

export function chunkEpubTtsBlocks(blocks: EpubTtsBlock[], options: number | EpubTtsChunkOptions = {}): EpubTtsChunk[] {
  const normalizedOptions = typeof options === "number" ? { firstSegmentMax: options, segmentMax: options } : options;
  const firstSegmentMax = normalizedOptions.firstSegmentMax ?? 280;
  const segmentMax = normalizedOptions.segmentMax ?? Math.max(firstSegmentMax, 500);
  if (normalizedOptions.preserveBlockBoundaries && blocks.length > 1) {
    return blocks.flatMap((block) => chunkEpubTtsBlocks([block], { firstSegmentMax, segmentMax }));
  }
  const units = blocks.flatMap((block) => splitBlockIntoUnits(block, segmentMax));
  const chunks: EpubTtsChunk[] = [];
  let index = 0;

  while (index < units.length) {
    const maxCharacters = chunks.length === 0 ? firstSegmentMax : segmentMax;
    const { consumed, segment, units: chunkUnits } = joinUnits(units.slice(index), maxCharacters);
    if (!segment || consumed === 0) break;

    let cursor = 0;
    const markers = chunkUnits.map((unit) => {
      const start = cursor;
      const end = cursor + unit.text.length;
      cursor = end + 1;
      return {
        blockId: unit.blockId,
        end,
        locatorText: unit.locatorText,
        sourceEnd: unit.sourceEnd,
        sourceStart: unit.sourceStart,
        spineItemId: unit.spineItemId,
        start,
        text: unit.text,
      };
    });

    chunks.push({ markers, text: segment });
    index += consumed;
  }

  return chunks;
}

export function createEpubTtsQueue({ client, onStateChange }: EpubTtsQueueDeps) {
  let state: EpubTtsQueueState = idleEpubTtsState();
  let runId = 0;

  const normalizeChunk = (chunk: EpubTtsQueueStartArgs["chunks"][number]): EpubTtsChunk =>
    typeof chunk === "string"
      ? { markers: [{ end: chunk.length, start: 0, text: chunk }], text: chunk }
      : chunk;

  const emitState = (nextState: EpubTtsQueueState) => {
    state = nextState;
    onStateChange?.(nextState);
  };

  const resolveMarker = (chunk: EpubTtsChunk, charIndex = 0) => {
    const normalizedIndex = Math.max(0, Math.min(charIndex, chunk.text.length));
    const markerIndex = chunk.markers.findIndex((candidate) => normalizedIndex >= candidate.start && normalizedIndex <= candidate.end);
    const resolvedMarkerIndex = markerIndex >= 0 ? markerIndex : Math.max(0, chunk.markers.length - 1);
    const marker = chunk.markers[resolvedMarkerIndex];
    return {
      marker,
      markerIndex: resolvedMarkerIndex,
      markerText: marker?.text || chunk.text,
    };
  };

  const resolveHighlightText = (chunk: EpubTtsChunk, markerState: ReturnType<typeof resolveMarker>, charIndex = 0) => {
    const sourceText = markerState.marker?.text || chunk.text;
    if (!sourceText) return { endOffset: -1, startOffset: -1, text: markerState.markerText };
    const relativeIndex = markerState.marker
      ? clamp(charIndex - markerState.marker.start, 0, Math.max(0, sourceText.length - 1))
      : clamp(charIndex, 0, Math.max(0, sourceText.length - 1));
    const resolvedWord = resolveBoundaryWord(sourceText, relativeIndex);
    const hasSourceOffsets =
      typeof markerState.marker?.sourceStart === "number" && typeof markerState.marker?.sourceEnd === "number";
    const baseOffset = hasSourceOffsets ? markerState.marker?.sourceStart ?? 0 : 0;
    return {
      endOffset: hasSourceOffsets ? baseOffset + resolvedWord.end : -1,
      startOffset: hasSourceOffsets ? baseOffset + resolvedWord.start : -1,
      text: resolvedWord.text || markerState.markerText,
    };
  };

  const chunkUsesSingleHighlightTarget = (chunk: EpubTtsChunk) => {
    if (chunk.markers.length <= 1) return true;
    const firstMarker = chunk.markers[0];
    return chunk.markers.every((marker) => marker.blockId === firstMarker?.blockId && marker.spineItemId === firstMarker?.spineItemId);
  };

  async function speakChunk(chunks: EpubTtsChunk[], index: number, request: EpubTtsQueueStartArgs["request"], activeRunId: number) {
    if (activeRunId !== runId) return;
    const chunk = chunks[index];
    if (!chunk) {
      emitState(idleEpubTtsState());
      return;
    }

    const initialMarker = resolveMarker(chunk);
    const boundaryWords = resolveBoundaryWords(chunk.text);
    const fallbackMs = Math.max(0, request.initialMarkerFallbackMs ?? initialMarkerFallbackMs);
    const revealInitialMarkerOnStart = chunkUsesSingleHighlightTarget(chunk);
    let initialMarkerVisible = false;
    let fallbackTimer: ReturnType<typeof setTimeout> | undefined;
    let positiveBoundarySeen = false;
    let zeroBoundaryWordIndex = -1;

    const clearFallbackTimer = () => {
      if (!fallbackTimer) return;
      clearTimeout(fallbackTimer);
      fallbackTimer = undefined;
    };

    const stateForMarker = (
      status: EpubTtsQueueState["status"],
      markerState: ReturnType<typeof resolveMarker>,
      highlight = resolveHighlightText(chunk, markerState, markerState.marker?.start ?? 0),
    ): EpubTtsQueueState => ({
      chunkIndex: index,
      currentText: chunk.text,
      markerBlockId: markerState.marker?.blockId ?? "",
      markerEndOffset: highlight.endOffset,
      markerIndex: markerState.markerIndex,
      markerLocatorText: markerState.marker?.locatorText ?? markerState.marker?.text ?? chunk.text,
      markerStartOffset: highlight.startOffset,
      markerText: highlight.text,
      spineItemId: markerState.marker?.spineItemId ?? "",
      status,
    });

    const revealInitialMarker = () => {
      if (activeRunId !== runId || initialMarkerVisible) return;
      initialMarkerVisible = true;
      emitState(stateForMarker("playing", initialMarker, resolveHighlightText(chunk, initialMarker, 0)));
    };

    emitState({
      ...idleEpubTtsState(),
      chunkIndex: index,
      currentText: chunk.text,
      status: "loading",
    });

    try {
      await client.speakSelection(chunk.text, {
        ...request,
        onStart: () => {
          if (activeRunId !== runId) return;
          if (revealInitialMarkerOnStart) {
            revealInitialMarker();
            return;
          }
          emitState({ ...state, chunkIndex: index, currentText: chunk.text, status: "playing" });
          clearFallbackTimer();
          fallbackTimer = setTimeout(revealInitialMarker, fallbackMs);
        },
        onBoundary: (event) => {
          if (activeRunId !== runId || !isWordBoundaryEvent(event)) return;
          clearFallbackTimer();
          initialMarkerVisible = true;
          const boundaryCharIndex = normalizeBoundaryCharIndex(event);
          let highlightCharIndex = boundaryCharIndex;
          if (boundaryCharIndex > 0) {
            positiveBoundarySeen = true;
            const matchingWordIndex = resolveBoundaryWordIndex(boundaryWords, boundaryCharIndex);
            if (matchingWordIndex >= 0) zeroBoundaryWordIndex = matchingWordIndex;
          } else if (boundaryWords.length) {
            zeroBoundaryWordIndex = Math.min(zeroBoundaryWordIndex + 1, boundaryWords.length - 1);
            highlightCharIndex = boundaryWords[zeroBoundaryWordIndex]?.start ?? 0;
          }
          const nextMarker = resolveMarker(chunk, highlightCharIndex);
          emitState(stateForMarker("playing", nextMarker, resolveHighlightText(chunk, nextMarker, highlightCharIndex)));
        },
        onEnd: () => {
          clearFallbackTimer();
          const pauseAfterMs = Math.max(0, chunk.pauseAfterMs ?? 0);
          if (pauseAfterMs > 0) {
            setTimeout(() => {
              if (activeRunId === runId) void speakChunk(chunks, index + 1, request, activeRunId);
            }, pauseAfterMs);
            return;
          }
          void speakChunk(chunks, index + 1, request, activeRunId);
        },
        onError: () => {
          clearFallbackTimer();
          if (activeRunId === runId) emitState(stateForMarker("error", initialMarker));
        },
      });
    } catch {
      clearFallbackTimer();
      if (activeRunId === runId) emitState(stateForMarker("error", initialMarker));
    }
  }

  return {
    getState() {
      return state;
    },
    pause() {
      if (state.status !== "playing") return;
      client.pause();
      emitState({ ...state, status: "paused" });
    },
    async resume() {
      if (state.status !== "paused") return;
      client.resume();
      emitState({ ...state, status: "playing" });
    },
    async start({ chunks, request }: EpubTtsQueueStartArgs) {
      runId += 1;
      const activeRunId = runId;
      const normalizedChunks = chunks.map(normalizeChunk).filter((chunk) => chunk.text.trim());
      if (!normalizedChunks.length) {
        emitState(idleEpubTtsState());
        return;
      }
      await speakChunk(normalizedChunks, 0, request, activeRunId);
    },
    stop() {
      runId += 1;
      client.stop();
      emitState(idleEpubTtsState());
    },
  };
}

export function idleEpubTtsState(): EpubTtsQueueState {
  return {
    chunkIndex: -1,
    currentText: "",
    markerBlockId: "",
    markerEndOffset: -1,
    markerIndex: -1,
    markerLocatorText: "",
    markerStartOffset: -1,
    markerText: "",
    spineItemId: "",
    status: "idle",
  };
}

export function applyEpubTtsSegment(doc: Document, segment: EpubTtsActiveSegment | null) {
  clearEpubTtsSegment(doc);
  if (!segment?.text) return null;
  if (segment.spineItemId && doc.documentElement.dataset.foliospaceTtsSpineItemId !== segment.spineItemId) return null;

  const block = findTtsBlockElement(doc, segment);
  if (!block) return null;
  const range = findTtsSegmentRange(block, segment);
  if (!range) {
    block.classList.add(ttsActiveClass);
    activeTtsElements.set(doc, block);
    return block;
  }

  try {
    const wrapper = doc.createElement("span");
    wrapper.className = ttsActiveClass;
    wrapper.append(range.extractContents());
    range.insertNode(wrapper);
    activeTtsElements.set(doc, wrapper);
    return wrapper;
  } catch {
    block.classList.add(ttsActiveClass);
    activeTtsElements.set(doc, block);
    return block;
  }
}

export function clearEpubTtsSegment(doc: Document) {
  const active = activeTtsElements.get(doc);
  if (!active) return;
  if (active.tagName.toLowerCase() === "span" && active.classList.contains(ttsActiveClass)) {
    const parent = active.parentNode;
    if (parent) {
      while (active.firstChild) parent.insertBefore(active.firstChild, active);
      parent.removeChild(active);
      parent.normalize();
    }
  } else {
    active.classList.remove(ttsActiveClass);
  }
  activeTtsElements.delete(doc);
}

function splitIntoSentences(paragraph: string) {
  return paragraph
    .split(/(?<=[.!?。！？])\s+/)
    .map((sentence) => sentence.trim())
    .filter(Boolean);
}

function splitOversizedSentence(sentence: string, maxCharacters: number) {
  const words = sentence.split(/\s+/).filter(Boolean);
  const chunks: string[] = [];
  let current = "";
  for (const word of words) {
    const candidate = current ? `${current} ${word}` : word;
    if (candidate.length <= maxCharacters || !current) {
      current = candidate;
      continue;
    }
    chunks.push(current);
    current = word;
  }
  if (current) chunks.push(current);
  return chunks;
}

function splitBlockIntoUnits(block: EpubTtsBlock, maxCharacters: number): EpubTtsBlock[] {
  if (block.text.length <= maxCharacters) return [block];
  const sentences = splitIntoSentences(block.text);
  let cursor = 0;
  return sentences.flatMap((sentence) => {
    const sentenceStart = block.text.indexOf(sentence, cursor);
    const resolvedSentenceStart = sentenceStart >= 0 ? sentenceStart : cursor;
    const sentenceEnd = resolvedSentenceStart + sentence.length;
    cursor = sentenceEnd;
    if (sentence.length <= maxCharacters) {
      return [{ ...block, sourceEnd: block.sourceStart + sentenceEnd, sourceStart: block.sourceStart + resolvedSentenceStart, text: sentence }];
    }
    let sentenceCursor = resolvedSentenceStart;
    return splitOversizedSentence(sentence, maxCharacters).map((unitText) => {
      const unitStart = block.text.indexOf(unitText, sentenceCursor);
      const resolvedUnitStart = unitStart >= 0 ? unitStart : sentenceCursor;
      const unitEnd = resolvedUnitStart + unitText.length;
      sentenceCursor = unitEnd;
      return { ...block, sourceEnd: block.sourceStart + unitEnd, sourceStart: block.sourceStart + resolvedUnitStart, text: unitText };
    });
  });
}

function joinUnits(units: EpubTtsBlock[], maxCharacters: number) {
  let current = "";
  let consumed = 0;
  const segmentUnits: EpubTtsBlock[] = [];

  for (const unit of units) {
    const candidate = current ? `${current} ${unit.text}` : unit.text;
    if (candidate.length <= maxCharacters || !current) {
      current = candidate;
      consumed += 1;
      segmentUnits.push(unit);
      continue;
    }
    break;
  }

  return { consumed, segment: current, units: segmentUnits };
}

function clamp(value: number, min: number, max: number) {
  return Math.max(min, Math.min(value, max));
}

function isSpokenTokenCharacter(character: string) {
  return /[\p{L}\p{N}'’-]/u.test(character);
}

function isWordBoundaryEvent(event: SpeechSynthesisEvent) {
  const boundaryName = typeof event.name === "string" ? event.name.toLowerCase() : "";
  return !boundaryName || boundaryName === "word";
}

function normalizeBoundaryCharIndex(event: SpeechSynthesisEvent) {
  const charIndex = typeof event.charIndex === "number" && Number.isFinite(event.charIndex) ? event.charIndex : 0;
  return Math.max(0, charIndex);
}

function resolveBoundaryWords(text: string) {
  const words: Array<{ end: number; start: number; text: string }> = [];
  let cursor = 0;
  while (cursor < text.length) {
    while (cursor < text.length && !isSpokenTokenCharacter(text[cursor] ?? "")) cursor += 1;
    if (cursor >= text.length) break;
    const start = cursor;
    while (cursor < text.length && isSpokenTokenCharacter(text[cursor] ?? "")) cursor += 1;
    const end = cursor;
    words.push({ end, start, text: text.slice(start, end) });
  }
  return words;
}

function resolveBoundaryWordIndex(words: Array<{ end: number; start: number; text: string }>, charIndex: number) {
  if (!words.length) return -1;
  let previousWordIndex = -1;
  for (let index = 0; index < words.length; index += 1) {
    const word = words[index];
    if (charIndex >= word.start && charIndex < word.end) return index;
    if (charIndex >= word.start) previousWordIndex = index;
  }
  return previousWordIndex >= 0 ? previousWordIndex : 0;
}

function resolveBoundaryWord(text: string, charIndex = 0) {
  const normalizedText = text.trim();
  if (!normalizedText) return { end: 0, start: 0, text: "" };
  let cursor = clamp(charIndex, 0, Math.max(0, text.length - 1));
  if (!isSpokenTokenCharacter(text[cursor] ?? "")) {
    let forward = cursor;
    while (forward < text.length && !isSpokenTokenCharacter(text[forward] ?? "")) forward += 1;
    if (forward < text.length) {
      cursor = forward;
    } else {
      let backward = cursor;
      while (backward >= 0 && !isSpokenTokenCharacter(text[backward] ?? "")) backward -= 1;
      if (backward < 0) return { end: normalizedText.length, start: 0, text: normalizedText };
      cursor = backward;
    }
  }

  let start = cursor;
  while (start > 0 && isSpokenTokenCharacter(text[start - 1] ?? "")) start -= 1;
  let end = cursor + 1;
  while (end < text.length && isSpokenTokenCharacter(text[end] ?? "")) end += 1;
  const resolvedText = text.slice(start, end).trim() || normalizedText;
  return { end: start + resolvedText.length, start, text: resolvedText };
}

function ensureTtsBlockID(element: HTMLElement, fallbackIndex: number) {
  const existing = element.getAttribute(ttsBlockIDAttribute);
  if (existing) return existing;
  const id = `epub-tts-block-${fallbackIndex}-${Math.random().toString(36).slice(2, 8)}`;
  element.setAttribute(ttsBlockIDAttribute, id);
  return id;
}

function collectElementTtsText(element: Element): EpubTtsTextResult {
  const positions: EpubTtsTextPosition[] = [];
  let text = "";
  let pendingSpace = false;

  const appendText = (node: Text) => {
    for (let offset = 0; offset < node.data.length; offset += 1) {
      const character = node.data[offset];
      if (/\s/.test(character)) {
        pendingSpace = true;
        continue;
      }
      if (pendingSpace && text && !text.endsWith(" ")) {
        text += " ";
      }
      positions.push({
        node,
        nodeOffset: offset,
        normalizedOffset: text.length,
        sourceOffset: text.length,
      });
      text += character;
      pendingSpace = false;
    }
  };

  const visit = (node: Node) => {
    if (node.nodeType === Node.ELEMENT_NODE) {
      const childElement = node as Element;
      if (isOmittableTtsElement(childElement)) return;
      Array.from(childElement.childNodes).forEach(visit);
      return;
    }
    if (node.nodeType === Node.TEXT_NODE) {
      appendText(node as Text);
    }
  };

  visit(element);
  return {
    positions,
    text: text.trim(),
  };
}

function isOmittableTtsElement(element: Element) {
  const tagName = element.tagName.toLowerCase();
  const text = element.textContent?.trim() ?? "";
  if (tagName === "sup") return true;
  if (/^(?:\[\d+\]|\d+:\d+|\d+)$/.test(text)) return true;
  if (tagName === "b" && /^v\d{6,}$/.test(element.id || "")) return true;
  if (element.getAttribute("epub:type") === "noteref") return true;
  return false;
}

function findTtsBlockElement(doc: Document, segment: EpubTtsActiveSegment) {
  if (segment.blockId) {
    const exact = doc.querySelector<HTMLElement>(`[${ttsBlockIDAttribute}="${cssEscape(segment.blockId)}"]`);
    if (exact) return exact;
  }

  const needle = (segment.locatorText || segment.text).replace(/\s+/g, " ").trim();
  if (!needle) return null;
  return Array.from(doc.querySelectorAll<HTMLElement>(ttsBlockSelector)).find((element) => {
    const text = collectElementTtsText(element).text;
    return text === needle || text.includes(needle) || needle.includes(text) || text.startsWith(needle.slice(0, 120));
  }) ?? null;
}

function findTtsSegmentRange(element: Element, segment: EpubTtsActiveSegment) {
  const result = collectElementTtsText(element);
  if (!result.text || result.positions.length === 0) return null;
  let start = typeof segment.startOffset === "number" && segment.startOffset >= 0 ? segment.startOffset : -1;
  let end = typeof segment.endOffset === "number" && segment.endOffset > start ? segment.endOffset : -1;
  if (start < 0 || end < 0) {
    start = result.text.indexOf(segment.text);
    end = start >= 0 ? start + segment.text.length : -1;
  }
  if (start < 0 || end <= start) return null;
  const startPosition = result.positions.find((position) => position.sourceOffset >= start);
  const endPosition = [...result.positions].reverse().find((position) => position.sourceOffset < end);
  if (!startPosition?.node || typeof startPosition.nodeOffset !== "number" || !endPosition?.node || typeof endPosition.nodeOffset !== "number") return null;

  const range = element.ownerDocument.createRange();
  range.setStart(startPosition.node, startPosition.nodeOffset);
  range.setEnd(endPosition.node, endPosition.nodeOffset + 1);
  return range;
}

function cssEscape(value: string) {
  if (typeof CSS !== "undefined" && typeof CSS.escape === "function") return CSS.escape(value);
  return value.replace(/["\\]/g, "\\$&");
}
