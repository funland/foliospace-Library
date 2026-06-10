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
  skipCancel?: boolean;
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
  onComplete?: () => void;
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
const voiceLoadFallbackMs = 500;
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
    let settled = false;
    const handleVoicesChanged = () => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      speechSynthesis.removeEventListener("voiceschanged", handleVoicesChanged);
      resolve(speechSynthesis.getVoices());
    };
    const timer = setTimeout(() => {
      if (settled) return;
      settled = true;
      speechSynthesis.removeEventListener("voiceschanged", handleVoicesChanged);
      resolve(speechSynthesis.getVoices());
    }, voiceLoadFallbackMs);
    speechSynthesis.addEventListener("voiceschanged", handleVoicesChanged);
  });
}

export function createBrowserTtsClient({
  speechSynthesis = globalThis.speechSynthesis as SpeechSynthesisLike | undefined,
  utteranceFactory = (text) => new SpeechSynthesisUtterance(text),
}: BrowserTtsClientDeps = {}) {
  let cachedVoices: SpeechSynthesisVoice[] = [];

  async function getSpeechVoices() {
    if (!speechSynthesis) throw new Error("speechSynthesis unavailable");
    const immediate = speechSynthesis.getVoices();
    if (immediate.length) {
      cachedVoices = immediate;
      return immediate;
    }
    if (cachedVoices.length) return cachedVoices;
    const voices = await waitForVoices(speechSynthesis);
    if (voices.length) cachedVoices = voices;
    return voices;
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
      if (!options.skipCancel) {
        speechSynthesis.cancel();
      }
      speechSynthesis.speak(utterance);
      speechSynthesis.resume();
      setTimeout(() => speechSynthesis.resume(), 0);
      setTimeout(() => speechSynthesis.resume(), 250);
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
  const hasPageWindow = typeof options.pagePosition === "number" && typeof options.pageWidth === "number";
  const pageWidth = hasPageWindow ? Math.max(0, options.pageWidth as number) : 0;
  const pageOrigin = hasPageWindow ? Math.max(0, (options.pagePosition as number) * pageWidth) : 0;
  const pageStart = hasPageWindow ? Math.max(0, pageOrigin - 2) : 0;
  const pageEnd = hasPageWindow ? pageOrigin + pageWidth + 2 : 0;
  const blocks = Array.from(doc.querySelectorAll<HTMLElement>(ttsBlockSelector))
    .map((element, index) => {
      const result = collectElementTtsText(element);
      return result.text ? { element, index, result } : null;
    })
    .filter((entry): entry is { element: HTMLElement; index: number; result: EpubTtsTextResult } => Boolean(entry));
  const start = hasPageWindow ? resolvePagedTtsBlockStart(blocks, pageOrigin, pageStart, pageEnd) : null;
  const startBlockIndex = start?.blockIndex ?? 0;

  return blocks
    .slice(startBlockIndex)
    .map(({ element, index, result }, offsetIndex) => {
      const blockId = ensureTtsBlockID(element, index);
      const slice = offsetIndex === 0 ? ttsBlockSlice(result.text, start?.sourceStart ?? 0) : ttsBlockSlice(result.text, 0);
      if (!slice.text) return null;
      return {
        blockId,
        locatorText: result.text,
        sourceEnd: slice.sourceEnd,
        sourceStart: slice.sourceStart,
        spineItemId,
        text: slice.text,
      };
    })
    .filter((block): block is EpubTtsBlock => Boolean(block));
}

export function extractEpubTtsBlocksFromSelectionStart(
  doc: Document,
  spineItemId: string,
  range: Range,
): EpubTtsBlock[] {
  const elements = Array.from(doc.querySelectorAll<HTMLElement>(ttsBlockSelector));
  const startElement = ttsBlockElementForNode(range.startContainer);
  const startIndex = startElement ? elements.indexOf(startElement) : -1;
  if (startIndex < 0) return [];

  return elements
    .slice(startIndex)
    .map((element, offsetIndex) => {
      const result = collectElementTtsText(element);
      if (!result.text) return null;
      const blockId = ensureTtsBlockID(element, startIndex + offsetIndex);
      const slice = offsetIndex === 0 ? ttsBlockSliceFromSelectionStart(result, range) : ttsBlockSlice(result.text, 0);
      if (!slice.text) return null;
      return {
        blockId,
        locatorText: result.text,
        sourceEnd: slice.sourceEnd,
        sourceStart: slice.sourceStart,
        spineItemId,
        text: slice.text,
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

export function createEpubTtsQueue({ client, onComplete, onStateChange }: EpubTtsQueueDeps) {
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
      onComplete?.();
      return;
    }

    const initialMarker = resolveMarker(chunk);
    const boundaryWords = resolveBoundaryWords(chunk.text);
    const fallbackMs = Math.max(0, request.initialMarkerFallbackMs ?? initialMarkerFallbackMs);
    const revealInitialMarkerOnStart = chunkUsesSingleHighlightTarget(chunk);
    let initialMarkerVisible = false;
    let fallbackTimer: ReturnType<typeof setTimeout> | undefined;
    let positiveBoundarySeen = false;
    let chunkSettled = false;
    let zeroBoundaryWordIndex = -1;

    const isActiveChunk = () => activeRunId === runId && !chunkSettled;

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
      if (!isActiveChunk() || initialMarkerVisible) return;
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
          if (!isActiveChunk()) return;
          if (revealInitialMarkerOnStart) {
            revealInitialMarker();
            return;
          }
          emitState({ ...state, chunkIndex: index, currentText: chunk.text, status: "playing" });
          clearFallbackTimer();
          fallbackTimer = setTimeout(revealInitialMarker, fallbackMs);
        },
        onBoundary: (event) => {
          if (!isActiveChunk() || !isWordBoundaryEvent(event)) return;
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
          if (activeRunId !== runId || chunkSettled) return;
          chunkSettled = true;
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
          if (activeRunId === runId && !chunkSettled) {
            chunkSettled = true;
            emitState(stateForMarker("error", initialMarker));
          }
        },
      });
    } catch {
      clearFallbackTimer();
      if (activeRunId === runId && !chunkSettled) {
        chunkSettled = true;
        emitState(stateForMarker("error", initialMarker));
      }
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

function ttsBlockElementForNode(node: Node | null) {
  if (!node) return null;
  const element = node.nodeType === Node.ELEMENT_NODE ? node as Element : node.parentElement;
  return element?.closest<HTMLElement>(ttsBlockSelector) ?? null;
}

function ttsBlockSliceFromSelectionStart(result: EpubTtsTextResult, range: Range) {
  const offset = selectionStartOffsetInTtsText(result, range);
  return ttsBlockSlice(result.text, resolveSelectionSpokenStart(result.text, offset));
}

function selectionStartOffsetInTtsText(result: EpubTtsTextResult, range: Range) {
  for (const position of result.positions) {
    if (!position.node || typeof position.nodeOffset !== "number") continue;
    try {
      if (range.comparePoint(position.node, position.nodeOffset) >= 0) {
        return position.normalizedOffset;
      }
    } catch {
      // Ignore positions outside the range document.
    }
  }
  return result.text.length;
}

function resolveSelectionSpokenStart(text: string, offset: number) {
  let start = clamp(Math.max(0, Math.min(offset, text.length)), 0, text.length);
  while (start > 0 && isSpokenTokenCharacter(text[start - 1] ?? "")) start -= 1;
  return start;
}

function ttsBlockSlice(text: string, startOffset: number) {
  const rawStart = Math.max(0, Math.min(startOffset, text.length));
  const sliced = text.slice(rawStart);
  const leadingTrim = sliced.length - sliced.trimStart().length;
  const trailingTrim = sliced.length - sliced.trimEnd().length;
  const sourceStart = rawStart + leadingTrim;
  const sourceEnd = Math.max(sourceStart, text.length - trailingTrim);
  return {
    sourceEnd,
    sourceStart,
    text: text.slice(sourceStart, sourceEnd),
  };
}

function resolvePagedTtsBlockStart(
  blocks: Array<{ element: HTMLElement; index: number; result: EpubTtsTextResult }>,
  pageOrigin: number,
  pageStart: number,
  pageEnd: number,
) {
  let fallbackBlockIndex = -1;
  for (let blockIndex = 0; blockIndex < blocks.length; blockIndex += 1) {
    const { element, result } = blocks[blockIndex];
    const visibleOffset = firstVisibleTtsTextOffsetInPage(element, result, pageOrigin, pageStart, pageEnd);
    if (visibleOffset !== null) {
      return { blockIndex, sourceStart: visibleOffset };
    }
    const offsetLeft = Number((element as { offsetLeft?: number }).offsetLeft);
    if (!Number.isFinite(offsetLeft)) {
      return { blockIndex, sourceStart: 0 };
    }
    if (offsetLeft >= pageStart) {
      return { blockIndex, sourceStart: 0 };
    }
    fallbackBlockIndex = blockIndex;
  }
  return fallbackBlockIndex >= 0 ? { blockIndex: fallbackBlockIndex, sourceStart: 0 } : null;
}

function firstVisibleTtsTextOffsetInPage(
  element: HTMLElement,
  result: EpubTtsTextResult,
  pageOrigin: number,
  pageStart: number,
  pageEnd: number,
) {
  if (!elementOverlapsTtsPage(element, pageOrigin, pageStart, pageEnd)) return null;
  for (const position of result.positions) {
    if (!position.node || typeof position.nodeOffset !== "number") continue;
    const rects = textPositionClientRects(position.node, position.nodeOffset);
    if (rects.some((rect) => clientRectOverlapsTtsPage(rect, pageOrigin, pageStart, pageEnd))) {
      return resolveSelectionSpokenStart(result.text, position.normalizedOffset);
    }
  }
  return 0;
}

function elementOverlapsTtsPage(element: HTMLElement, pageOrigin: number, pageStart: number, pageEnd: number) {
  const rects = safeClientRects(element);
  if (rects.length > 0) {
    return rects.some((rect) => clientRectOverlapsTtsPage(rect, pageOrigin, pageStart, pageEnd));
  }
  const offsetLeft = Number((element as { offsetLeft?: number }).offsetLeft);
  const width = Number(element.offsetWidth || element.scrollWidth || element.clientWidth || 0);
  return Number.isFinite(offsetLeft) && Number.isFinite(width) && width > 0 && offsetLeft < pageEnd && offsetLeft + width > pageStart;
}

function textPositionClientRects(node: Node, nodeOffset: number) {
  const doc = (node as { ownerDocument?: Document }).ownerDocument;
  const range = doc?.createRange?.();
  const data = (node as { data?: string }).data ?? "";
  if (!range || !data) return [];
  try {
    const startOffset = Math.max(0, Math.min(nodeOffset, data.length));
    range.setStart(node, startOffset);
    range.setEnd(node, Math.min(startOffset + 1, data.length));
    return Array.from(range.getClientRects?.() ?? []);
  } catch {
    return [];
  } finally {
    range.detach?.();
  }
}

function safeClientRects(element: Element) {
  try {
    return Array.from(element.getClientRects?.() ?? []);
  } catch {
    return [];
  }
}

function clientRectOverlapsTtsPage(rect: DOMRect, pageOrigin: number, pageStart: number, pageEnd: number) {
  const left = Number(rect.left) + pageOrigin;
  const right = Number(rect.right) + pageOrigin;
  return Number.isFinite(left) && Number.isFinite(right) && right > pageStart && left < pageEnd;
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
