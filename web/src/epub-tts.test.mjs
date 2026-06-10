import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";
import ts from "typescript";

const srcDir = path.dirname(fileURLToPath(import.meta.url));

async function loadEpubTtsModule() {
  const source = await readFile(path.join(srcDir, "epub-tts.ts"), "utf8");
  const transpiled = ts.transpileModule(source, {
    compilerOptions: {
      module: ts.ModuleKind.ES2020,
      target: ts.ScriptTarget.ES2020,
    },
  }).outputText;
  return import(`data:text/javascript;base64,${Buffer.from(transpiled).toString("base64")}`);
}

function withFakeDOMNodeConstants(fn) {
  const previousNode = globalThis.Node;
  globalThis.Node = { ELEMENT_NODE: 1, TEXT_NODE: 3 };
  try {
    return fn();
  } finally {
    if (previousNode === undefined) {
      delete globalThis.Node;
    } else {
      globalThis.Node = previousNode;
    }
  }
}

function fakeParagraphDocument(paragraphs) {
  const document = {
    createRange() {
      let startNode = null;
      let startOffset = 0;
      return {
        detach() {},
        getClientRects() {
          return startNode?.rectsForOffset?.(startOffset) ?? [];
        },
        setEnd() {},
        setStart(node, offset) {
          startNode = node;
          startOffset = offset;
        },
      };
    },
    querySelectorAll() {
      return elements;
    },
  };
  const elements = paragraphs.map((paragraph) => {
    const attrs = new Map();
    const textNode = {
      data: paragraph.text,
      nodeType: 3,
      ownerDocument: document,
      rectsForOffset: paragraph.rectsForOffset,
    };
    return {
      childNodes: [textNode],
      clientWidth: paragraph.clientWidth ?? 0,
      getAttribute(name) {
        return attrs.get(name) ?? "";
      },
      getClientRects() {
        return paragraph.elementRects ?? [];
      },
      nodeType: 1,
      offsetLeft: paragraph.offsetLeft,
      offsetWidth: paragraph.offsetWidth ?? 0,
      ownerDocument: document,
      scrollWidth: paragraph.scrollWidth ?? 0,
      setAttribute(name, value) {
        attrs.set(name, value);
      },
      tagName: "p",
      textContent: paragraph.text,
    };
  });
  return document;
}

test("epub tts text normalization omits bible verse and footnote markers", async () => {
  const { normalizeEpubTtsText } = await loadEpubTtsModule();

  const normalized = normalizeEpubTtsText([
    { text: "20", omit: true },
    { text: "And God said" },
    { text: "[1]", omit: true },
    { text: "let there be light." },
  ]);

  assert.equal(normalized.text, "And God said let there be light.");
  assert.equal(normalized.positions[0].sourceOffset, 0);
  assert.equal(normalized.positions.at(-1).normalizedOffset, normalized.text.length - 1);
});

test("epub tts chunks preserve source offsets for spoken word highlighting", async () => {
  const { chunkEpubTtsBlocks } = await loadEpubTtsModule();

  const chunks = chunkEpubTtsBlocks([
    {
      blockId: "block-1",
      locatorText: "Alpha beta gamma.",
      sourceEnd: 17,
      sourceStart: 0,
      spineItemId: "chapter-1.xhtml",
      text: "Alpha beta gamma.",
    },
  ]);

  assert.equal(chunks[0].text, "Alpha beta gamma.");
  assert.deepEqual(chunks[0].markers[0], {
    end: 17,
    locatorText: "Alpha beta gamma.",
    sourceEnd: 17,
    sourceStart: 0,
    spineItemId: "chapter-1.xhtml",
    start: 0,
    text: "Alpha beta gamma.",
    blockId: "block-1",
  });
});

test("epub tts chunks can preserve block boundaries for paginated Chrome playback", async () => {
  const { chunkEpubTtsBlocks } = await loadEpubTtsModule();

  const chunks = chunkEpubTtsBlocks(
    [
      {
        blockId: "block-1",
        locatorText: "First paragraph should become its own utterance.",
        sourceEnd: 47,
        sourceStart: 0,
        spineItemId: "chapter-1.xhtml",
        text: "First paragraph should become its own utterance.",
      },
      {
        blockId: "block-2",
        locatorText: "Second paragraph should not be merged into the first.",
        sourceEnd: 52,
        sourceStart: 0,
        spineItemId: "chapter-1.xhtml",
        text: "Second paragraph should not be merged into the first.",
      },
    ],
    { preserveBlockBoundaries: true },
  );

  assert.equal(chunks.length, 2);
  assert.equal(chunks[0].text, "First paragraph should become its own utterance.");
  assert.equal(chunks[0].markers[0].blockId, "block-1");
  assert.equal(chunks[1].text, "Second paragraph should not be merged into the first.");
  assert.equal(chunks[1].markers[0].blockId, "block-2");
});

test("epub tts extraction starts from a paragraph fragment visible on the current page", async () => {
  const { extractEpubTtsBlocks } = await loadEpubTtsModule();
  const doc = fakeParagraphDocument([
    {
      elementRects: [{ left: -80, right: 96 }, { left: 10, right: 180 }],
      offsetLeft: 0,
      rectsForOffset: (offset) => offset < 11 ? [{ left: -80, right: -79 }] : [{ left: 10, right: 11 }],
      text: "Alpha beta gamma delta.",
    },
    {
      elementRects: [{ left: 190, right: 260 }],
      offsetLeft: 290,
      rectsForOffset: () => [{ left: 190, right: 191 }],
      text: "Next paragraph.",
    },
  ]);

  const blocks = await withFakeDOMNodeConstants(() =>
    extractEpubTtsBlocks(doc, "chapter.xhtml", { pagePosition: 1, pageWidth: 100 }),
  );

  assert.equal(blocks[0].locatorText, "Alpha beta gamma delta.");
  assert.equal(blocks[0].text, "gamma delta.");
  assert.equal(blocks[0].sourceStart, 11);
  assert.equal(blocks[1].text, "Next paragraph.");
});

test("browser tts speaks with the default voice when voiceschanged never fires", async () => {
  const { createBrowserTtsClient } = await loadEpubTtsModule();
  const spoken = [];
  const listeners = new Set();
  const speechSynthesis = {
    addEventListener(name, listener) {
      if (name === "voiceschanged") listeners.add(listener);
    },
    cancel() {},
    getVoices() {
      return [];
    },
    pause() {},
    removeEventListener(_name, listener) {
      listeners.delete(listener);
    },
    resume() {},
    speak(utterance) {
      spoken.push(utterance);
    },
  };
  const client = createBrowserTtsClient({
    speechSynthesis,
    utteranceFactory: (text) => ({ text }),
  });

  await client.speakSelection("Next chapter", {
    onEnd() {},
    onError() {},
    rate: 1,
    voiceId: "",
    volume: 1,
  });

  assert.equal(spoken.length, 1);
  assert.equal(spoken[0].text, "Next chapter");
  assert.equal(spoken[0].voice, null);
  assert.equal(listeners.size, 0);
});

test("browser tts reuses cached voices when Chrome temporarily returns an empty voice list", async () => {
  const { createBrowserTtsClient } = await loadEpubTtsModule();
  const voice = { default: true, lang: "en-US", localService: true, name: "Google US English", voiceURI: "google-en" };
  const spoken = [];
  let getVoiceCalls = 0;
  const speechSynthesis = {
    addEventListener() {},
    cancel() {},
    getVoices() {
      getVoiceCalls += 1;
      return getVoiceCalls === 1 ? [voice] : [];
    },
    pause() {},
    removeEventListener() {},
    resume() {},
    speak(utterance) {
      spoken.push(utterance);
    },
  };
  const client = createBrowserTtsClient({
    speechSynthesis,
    utteranceFactory: (text) => ({ text }),
  });

  const voices = await client.getVoices();
  assert.equal(voices[0].id, "google-en");
  await client.speakSelection("Introduction", {
    onEnd() {},
    onError() {},
    rate: 1,
    voiceId: "google-en",
    volume: 1,
  });

  assert.equal(spoken.length, 1);
  assert.equal(spoken[0].voice, voice);
});

test("browser tts can continue an active speech session without resetting the queue", async () => {
  const { createBrowserTtsClient } = await loadEpubTtsModule();
  const spoken = [];
  let cancelCount = 0;
  let resumeCount = 0;
  const speechSynthesis = {
    addEventListener() {},
    cancel() {
      cancelCount += 1;
    },
    getVoices() {
      return [];
    },
    pause() {},
    removeEventListener() {},
    resume() {
      resumeCount += 1;
    },
    speak(utterance) {
      spoken.push(utterance);
    },
  };
  const client = createBrowserTtsClient({
    speechSynthesis,
    utteranceFactory: (text) => ({ text }),
  });

  await client.speakSelection("Next chapter", {
    onEnd() {},
    onError() {},
    rate: 1,
    skipCancel: true,
    voiceId: "",
    volume: 1,
  });

  assert.equal(cancelCount, 0);
  assert.equal(spoken.length, 1);
  assert.equal(spoken[0].text, "Next chapter");
  assert.ok(resumeCount > 0);
});

test("epub tts queue maps browser word boundaries to current marker text", async () => {
  const { createEpubTtsQueue } = await loadEpubTtsModule();
  const states = [];
  let currentRequest;
  const client = {
    pause() {},
    resume() {},
    speakSelection(_text, request) {
      currentRequest = request;
      return Promise.resolve();
    },
    stop() {},
  };
  const queue = createEpubTtsQueue({
    client,
    onStateChange: (state) => states.push(state),
  });

  await queue.start({
    chunks: [
      {
        markers: [
          {
            blockId: "block-1",
            end: 16,
            locatorText: "Alpha beta gamma",
            sourceEnd: 16,
            sourceStart: 0,
            spineItemId: "chapter-1.xhtml",
            start: 0,
            text: "Alpha beta gamma",
          },
        ],
        text: "Alpha beta gamma",
      },
    ],
    request: { rate: 1, voiceId: "", volume: 1 },
  });

  currentRequest.onStart();
  currentRequest.onBoundary({ charIndex: 6, name: "word" });

  assert.equal(states.at(-1).status, "playing");
  assert.equal(states.at(-1).markerText, "beta");
  assert.equal(states.at(-1).markerStartOffset, 6);
  assert.equal(states.at(-1).markerEndOffset, 10);
  assert.equal(states.at(-1).markerBlockId, "block-1");
});

test("epub tts queue reports natural completion after the final chunk only", async () => {
  const { createEpubTtsQueue } = await loadEpubTtsModule();
  const completed = [];
  const requests = [];
  const client = {
    pause() {},
    resume() {},
    speakSelection(_text, request) {
      requests.push(request);
      return Promise.resolve();
    },
    stop() {},
  };
  const queue = createEpubTtsQueue({
    client,
    onComplete: () => completed.push("complete"),
  });

  await queue.start({
    chunks: ["First chunk.", "Second chunk."],
    request: { rate: 1, voiceId: "", volume: 1 },
  });

  requests[0].onEnd();
  assert.deepEqual(completed, []);
  requests[1].onEnd();
  assert.deepEqual(completed, ["complete"]);

  await queue.start({
    chunks: ["Manual stop should not complete."],
    request: { rate: 1, voiceId: "", volume: 1 },
  });
  queue.stop();
  assert.deepEqual(completed, ["complete"]);
});

test("epub tts queue ignores stale errors from a naturally ended chunk", async () => {
  const { createEpubTtsQueue } = await loadEpubTtsModule();
  const states = [];
  const requests = [];
  const client = {
    pause() {},
    resume() {},
    speakSelection(_text, request) {
      requests.push(request);
      return Promise.resolve();
    },
    stop() {},
  };
  const queue = createEpubTtsQueue({
    client,
    onStateChange: (state) => states.push(state),
  });

  await queue.start({
    chunks: ["First chunk.", "Second chunk."],
    request: { rate: 1, voiceId: "", volume: 1 },
  });

  requests[0].onStart();
  requests[0].onEnd();
  requests[1].onStart();
  requests[0].onError({ error: "interrupted" });

  assert.equal(states.at(-1).status, "playing");
  assert.equal(states.at(-1).currentText, "Second chunk.");
});

test("epub tts queue keeps Google zero-index boundaries from resetting an active word", async () => {
  const { createEpubTtsQueue } = await loadEpubTtsModule();
  const states = [];
  let currentRequest;
  const client = {
    pause() {},
    resume() {},
    speakSelection(_text, request) {
      currentRequest = request;
      return Promise.resolve();
    },
    stop() {},
  };
  const queue = createEpubTtsQueue({
    client,
    onStateChange: (state) => states.push(state),
  });

  await queue.start({
    chunks: [
      {
        markers: [
          {
            blockId: "block-1",
            end: 22,
            locatorText: "Alpha beta gamma delta",
            sourceEnd: 22,
            sourceStart: 0,
            spineItemId: "chapter-1.xhtml",
            start: 0,
            text: "Alpha beta gamma delta",
          },
        ],
        text: "Alpha beta gamma delta",
      },
    ],
    request: { rate: 1, voiceId: "Google US English", volume: 1 },
  });

  currentRequest.onStart();
  currentRequest.onBoundary({ charIndex: 6, name: "word" });
  currentRequest.onBoundary({ charIndex: 0, name: "word" });

  const spokenWords = states.map((state) => state.markerText).filter(Boolean);
  assert.deepEqual(spokenWords.slice(-2), ["beta", "gamma"]);
  assert.equal(states.at(-1).markerStartOffset, 11);
  assert.equal(states.at(-1).markerEndOffset, 16);
  queue.stop();
});

test("epub tts queue maps repeated zero-index Chrome word boundaries by event order", async () => {
  const { createEpubTtsQueue } = await loadEpubTtsModule();
  const states = [];
  let currentRequest;
  const client = {
    pause() {},
    resume() {},
    speakSelection(_text, request) {
      currentRequest = request;
      return Promise.resolve();
    },
    stop() {},
  };
  const queue = createEpubTtsQueue({
    client,
    onStateChange: (state) => states.push(state),
  });

  await queue.start({
    chunks: [
      {
        markers: [
          {
            blockId: "block-1",
            end: 22,
            locatorText: "Alpha beta gamma delta",
            sourceEnd: 22,
            sourceStart: 0,
            spineItemId: "chapter-1.xhtml",
            start: 0,
            text: "Alpha beta gamma delta",
          },
        ],
        text: "Alpha beta gamma delta",
      },
    ],
    request: {
      rate: 1,
      voiceId: "",
      volume: 1,
    },
  });

  currentRequest.onStart();
  currentRequest.onBoundary({ charIndex: 0, name: "word" });
  currentRequest.onBoundary({ charIndex: 0, name: "word" });
  currentRequest.onBoundary({ charIndex: 0, name: "word" });
  currentRequest.onBoundary({ charIndex: 0, name: "word" });

  const spokenWords = states.map((state) => state.markerText).filter(Boolean);
  assert.deepEqual(spokenWords.slice(-4), ["Alpha", "beta", "gamma", "delta"]);
  assert.equal(states.at(-1).markerStartOffset, 17);
  assert.equal(states.at(-1).markerEndOffset, 22);
  queue.stop();
});

test("epub tts queue does not synthesize later word highlights without browser boundaries", async () => {
  const { createEpubTtsQueue } = await loadEpubTtsModule();
  const states = [];
  let currentRequest;
  const client = {
    pause() {},
    resume() {},
    speakSelection(_text, request) {
      currentRequest = request;
      return Promise.resolve();
    },
    stop() {},
  };
  const queue = createEpubTtsQueue({
    client,
    onStateChange: (state) => states.push(state),
  });

  await queue.start({
    chunks: [
      {
        markers: [
          {
            blockId: "block-1",
            end: 22,
            locatorText: "Alpha beta gamma delta",
            sourceEnd: 22,
            sourceStart: 0,
            spineItemId: "chapter-1.xhtml",
            start: 0,
            text: "Alpha beta gamma delta",
          },
        ],
        text: "Alpha beta gamma delta",
      },
    ],
    request: {
      rate: 1,
      voiceId: "",
      volume: 1,
    },
  });

  currentRequest.onStart();
  await new Promise((resolve) => setTimeout(resolve, 35));

  const spokenWords = states.map((state) => state.markerText).filter(Boolean);
  assert.equal(spokenWords[0], "Alpha");
  assert.equal(spokenWords.at(-1), "Alpha", "highlight should not guess later words when no browser boundary exists");
  queue.stop();
});

test("epub tts queue uses zero-index word boundary order when Chrome omits char offsets", async () => {
  const { createEpubTtsQueue } = await loadEpubTtsModule();
  const states = [];
  let currentRequest;
  const client = {
    pause() {},
    resume() {},
    speakSelection(_text, request) {
      currentRequest = request;
      return Promise.resolve();
    },
    stop() {},
  };
  const queue = createEpubTtsQueue({
    client,
    onStateChange: (state) => states.push(state),
  });

  await queue.start({
    chunks: [
      {
        markers: [
          {
            blockId: "block-1",
            end: 22,
            locatorText: "Alpha beta gamma delta",
            sourceEnd: 22,
            sourceStart: 0,
            spineItemId: "chapter-1.xhtml",
            start: 0,
            text: "Alpha beta gamma delta",
          },
        ],
        text: "Alpha beta gamma delta",
      },
    ],
    request: {
      rate: 1,
      voiceId: "",
      volume: 1,
    },
  });

  currentRequest.onStart();
  const zeroBoundaryTimer = setInterval(() => {
    currentRequest.onBoundary({ charIndex: 0, name: "word" });
  }, 5);
  await new Promise((resolve) => setTimeout(resolve, 45));
  clearInterval(zeroBoundaryTimer);

  const spokenWords = states.map((state) => state.markerText).filter(Boolean);
  assert.equal(spokenWords[0], "Alpha");
  assert.notEqual(spokenWords.at(-1), "Alpha");
  assert.ok(
    spokenWords.includes("beta") || spokenWords.includes("gamma") || spokenWords.includes("delta"),
    "repeated zero-index word boundaries should still advance without time-based estimates",
  );
  queue.stop();
});

test("epub tts queue follows repeated Chrome zero-index word boundaries instead of timed estimates", async () => {
  const { createEpubTtsQueue } = await loadEpubTtsModule();
  const states = [];
  let currentRequest;
  const client = {
    pause() {},
    resume() {},
    speakSelection(_text, request) {
      currentRequest = request;
      return Promise.resolve();
    },
    stop() {},
  };
  const queue = createEpubTtsQueue({
    client,
    onStateChange: (state) => states.push(state),
  });

  await queue.start({
    chunks: [
      {
        markers: [
          {
            blockId: "block-1",
            end: 22,
            locatorText: "Alpha beta gamma delta",
            sourceEnd: 22,
            sourceStart: 0,
            spineItemId: "chapter-1.xhtml",
            start: 0,
            text: "Alpha beta gamma delta",
          },
        ],
        text: "Alpha beta gamma delta",
      },
    ],
    request: {
      rate: 1,
      voiceId: "",
      volume: 1,
    },
  });

  currentRequest.onStart();
  const zeroBoundaryTimer = setInterval(() => {
    currentRequest.onBoundary({ charIndex: 0, name: "word" });
  }, 80);
  await new Promise((resolve) => setTimeout(resolve, 1150));
  clearInterval(zeroBoundaryTimer);

  const spokenWords = states.map((state) => state.markerText).filter(Boolean);
  assert.equal(spokenWords[0], "Alpha");
  assert.ok(spokenWords.includes("beta"), "fallback should still move beyond the first word");
  assert.equal(
    spokenWords.at(-1),
    "delta",
    "Chrome zero-index word boundaries should drive the spoken word instead of slower timer estimates",
  );
  queue.stop();
});

test("epub tts controls are placed directly after the back-to-shelf button", async () => {
  const appSource = await readFile(path.join(srcDir, "App.tsx"), "utf8");
  const styleSource = await readFile(path.join(srcDir, "styles.css"), "utf8");

  assert.match(
    appSource,
    /<button onClick=\{returnToLibrary\}>\{t\.backToShelf\}<\/button>\s*\{epubTtsFeatureEnabled && selectedBook\.format === "epub" && \(\s*<EpubTtsControls/s,
    "enabled EPUB TTS controls should be in the toolbar immediately after the back-to-shelf button",
  );
  assert.match(
    appSource,
    /onTtsDocumentReady=\{handleEpubTtsDocumentReady\}/,
    "EpubFrame should expose its iframe document to the TTS controller",
  );
  assert.match(
    styleSource,
    /\.epubTtsControls\s*\{[^}]*display:\s*inline-flex;/s,
    "EPUB TTS controls should be styled as a compact inline control group",
  );
  assert.match(
    styleSource,
    /\.reader:fullscreen \.epubTtsStatus/s,
    "EPUB TTS status should remain readable in fullscreen mode",
  );
});

test("epub tts controls remain reachable in fullscreen without restoring the full toolbar", async () => {
  const appSource = await readFile(path.join(srcDir, "App.tsx"), "utf8");
  const styleSource = await readFile(path.join(srcDir, "styles.css"), "utf8");

  assert.match(
    appSource,
    /\{readerFullscreen && epubTtsFeatureEnabled && selectedBook\.format === "epub" && \(\s*<div className="readerFullscreenTts"/s,
    "EPUB fullscreen should expose a dedicated TTS control island when TTS is enabled",
  );
  assert.match(
    styleSource,
    /\.readerFullscreenTts\s*\{[^}]*position:\s*fixed;[^}]*z-index:\s*40;/s,
    "fullscreen TTS controls should float above the immersive reading stage",
  );
  assert.match(
    styleSource,
    /\.readerFullscreenTts \.epubTtsControls\s*\{[^}]*background:\s*rgba\(18, 24, 27,/s,
    "fullscreen TTS controls should use a translucent immersive style",
  );
  assert.match(
    styleSource,
    /\.readerFullscreenTts\s*\{[^}]*opacity:\s*0\.78;/s,
    "fullscreen TTS controls should use the same subtle floating treatment as the exit button",
  );
  assert.match(
    styleSource,
    /\.readerFullscreenTts \.epubTtsControls\s*\{[^}]*border-radius:\s*999px;[^}]*background:\s*rgba\(18, 24, 27, 0\.38\);/s,
    "fullscreen TTS controls should visually match the exit fullscreen capsule",
  );
  assert.match(
    styleSource,
    /\.reader\.immersiveMode \.readerFullscreenTts \.epubTtsControls\s*\{[^}]*background:\s*rgba\(18, 24, 27, 0\.38\);/s,
    "fullscreen TTS controls should not be overridden by generic fullscreen toolbar styles",
  );
  assert.match(
    styleSource,
    /\.readerFullscreenTts:hover \.epubTtsControls,[\s\S]*\.readerFullscreenTts:focus-within \.epubTtsControls\s*\{[^}]*background:\s*rgba\(18, 24, 27, 0\.84\);/s,
    "fullscreen TTS hover state should mirror the exit fullscreen button hover background",
  );
  assert.match(
    styleSource,
    /\.reader\.immersiveMode \.readerHeader,[\s\S]*\.reader\.immersiveMode \.readerControls,[\s\S]*display:\s*none;/s,
    "immersive mode should keep the full reader toolbar hidden",
  );
});

test("epub contents remain reachable in fullscreen as a compact top-left control", async () => {
  const appSource = await readFile(path.join(srcDir, "App.tsx"), "utf8");
  const styleSource = await readFile(path.join(srcDir, "styles.css"), "utf8");

  assert.match(
    appSource,
    /\{readerFullscreen && selectedBook\.format === "epub" && epubManifest && \(\s*<div className="readerFullscreenContents"/s,
    "EPUB fullscreen should expose a dedicated contents button while the main header is hidden",
  );
  assert.match(
    appSource,
    /<button aria-expanded=\{epubTocOpen\} onClick=\{\(\) => setEpubTocOpen\(\(value\) => !value\)\}>\{t\.contents\}<\/button>/s,
    "the fullscreen contents button should toggle the existing EPUB TOC panel",
  );
  assert.match(
    styleSource,
    /\.readerFullscreenContents\s*\{[^}]*position:\s*fixed;[^}]*top:\s*calc\(10px \+ env\(safe-area-inset-top\)\);[^}]*left:\s*10px;[^}]*z-index:\s*40;/s,
    "fullscreen contents control should float in the same top-left layer as TTS",
  );
  assert.match(
    styleSource,
    /\.readerFullscreenContents button\s*\{[^}]*border-radius:\s*999px;[^}]*background:\s*rgba\(18, 24, 27, 0\.38\);/s,
    "fullscreen contents button should visually match the TTS fullscreen capsule",
  );
});

test("epub tts controls are gated by the server capability and hidden by default", async () => {
  const appSource = await readFile(path.join(srcDir, "App.tsx"), "utf8");

  assert.match(
    appSource,
    /const epubTtsFeatureEnabled = Boolean\(clientInfo\?\.capabilities\?\.epubTts\);/,
    "the Web reader should derive EPUB TTS visibility from the server capability",
  );
  assert.match(
    appSource,
    /\{epubTtsFeatureEnabled && selectedBook\.format === "epub" && \(\s*<EpubTtsControls/s,
    "the normal reader toolbar should only render TTS controls when EPUB TTS is enabled by config",
  );
  assert.match(
    appSource,
    /\{readerFullscreen && epubTtsFeatureEnabled && selectedBook\.format === "epub" && \(\s*<div className="readerFullscreenTts"/s,
    "fullscreen TTS controls should also be hidden unless EPUB TTS is enabled by config",
  );
  assert.match(
    appSource,
    /\{epubTtsFeatureEnabled && epubTtsSettingsOpen && \(\s*<EpubTtsSettingsPanel/s,
    "the TTS settings panel should not render when EPUB TTS is disabled",
  );
  assert.match(
    appSource,
    /useEffect\(\(\) => \{[\s\S]*if \(epubTtsFeatureEnabled\) return;[\s\S]*stopEpubTts\(\);[\s\S]*setEpubTtsSettingsOpen\(false\);[\s\S]*\}, \[epubTtsFeatureEnabled\]\);/,
    "turning off the server capability should stop active TTS and close its settings panel",
  );
});

test("epub toc collapses oversized nested chapter groups while marking the active parent", async () => {
  const appSource = await readFile(path.join(srcDir, "App.tsx"), "utf8");
  const styleSource = await readFile(path.join(srcDir, "styles.css"), "utf8");

  assert.match(
    appSource,
    /const \[expanded, setExpanded\] = useState\(defaultExpanded\);/,
    "nested EPUB TOC nodes should keep local expansion state",
  );
  assert.match(
    appSource,
    /children\.length > 0 && expanded && \(/,
    "nested EPUB TOC children should only render while their parent is expanded",
  );
  assert.match(
    appSource,
    /aria-expanded=\{hasChildren \? expanded : undefined\}/,
    "parent TOC rows should expose collapsed state to assistive technology",
  );
  assert.match(
    appSource,
    /childCount:\s*children\.length/s,
    "TOC expansion rules should know how many child entries would be revealed",
  );
  assert.match(
    appSource,
    /depth === 0 && itemCount >= 8 && childCount > 12\) return false;/,
    "Bible-style top-level sections with many chapters should stay collapsed by default",
  );
  assert.match(
    appSource,
    /const hasChildren = \(item\.children \?\? \[\]\)\.length > 0;/,
    "fragment fallback should know whether a TOC row is a parent group",
  );
  assert.match(
    appSource,
    /hasChildren && stripEPUBFragment\(item\.href\) === spineHref/,
    "Bible-style parent entries with fragment hrefs should match the current spine item without marking every chapter active",
  );
  assert.match(
    appSource,
    /childActive \? " containsActive" : ""/,
    "collapsed parents should still mark that they contain the active reading position",
  );
  assert.match(
    appSource,
    /<span className="epubTocChildCount">\{children\.length\}<\/span>/,
    "collapsed chapter groups should show how many entries are hidden",
  );
  assert.match(
    styleSource,
    /\.epubTocToggle\s*\{[^}]*flex:\s*0 0 28px;/s,
    "collapsible TOC groups should have a compact toggle affordance",
  );
  assert.match(
    styleSource,
    /\.epubTocItem\.expanded \.epubTocToggle::before\s*\{[^}]*transform:\s*rotate\(90deg\);/s,
    "expanded TOC groups should visibly rotate the disclosure indicator",
  );
});

test("epub tts settings are hidden by default and expand as a separate reader panel", async () => {
  const appSource = await readFile(path.join(srcDir, "App.tsx"), "utf8");
  const styleSource = await readFile(path.join(srcDir, "styles.css"), "utf8");

  assert.ok(
    appSource.includes("const [epubTtsSettingsOpen, setEpubTtsSettingsOpen] = useState(false);"),
    "EPUB TTS settings should be hidden by default",
  );
  assert.match(
    appSource,
    /onSettingsToggle=\{\(\) => setEpubTtsSettingsOpen\(\(value\) => !value\)\}/,
    "the compact TTS controls should toggle the settings panel",
  );
  assert.match(
    appSource,
    /\{epubTtsFeatureEnabled && epubTtsSettingsOpen && \(\s*<EpubTtsSettingsPanel/s,
    "the TTS settings panel should render separately from the compact toolbar controls when TTS is enabled",
  );
  assert.match(
    styleSource,
    /\.epubTtsSettings\s*\{[^}]*position:\s*absolute;[^}]*z-index:\s*5;[^}]*width:\s*min\(320px, calc\(100% - 36px\)\);/s,
    "TTS settings should use a TOC-like floating panel in the EPUB stage",
  );
});

test("epub tts settings block page edge hit zones while controls are being adjusted", async () => {
  const appSource = await readFile(path.join(srcDir, "App.tsx"), "utf8");
  const styleSource = await readFile(path.join(srcDir, "styles.css"), "utf8");

  assert.ok(
    appSource.includes('${epubTtsSettingsOpen ? " ttsSettingsOpen" : ""}'),
    "opening TTS settings should mark the reader stage so edge navigation can be suspended",
  );
  assert.match(
    appSource,
    /className="epubTtsSettings"[\s\S]*onPointerDown=\{\(event\) => event\.stopPropagation\(\)\}[\s\S]*onClick=\{\(event\) => event\.stopPropagation\(\)\}[\s\S]*onWheel=\{\(event\) => event\.stopPropagation\(\)\}/,
    "the settings panel should consume pointer, click, and wheel events instead of letting them reach reader navigation",
  );
  assert.match(
    appSource,
    /function startReaderSwipe\(x: number, y: number\) \{\s*if \(epubTtsSettingsOpen\) return;/,
    "drag-based page turns should not start while TTS settings are open",
  );
  assert.match(
    appSource,
    /function finishReaderSwipe\(x: number, y: number\) \{\s*if \(epubTtsSettingsOpen\) \{\s*swipeStart\.current = null;\s*return;\s*\}/,
    "an in-progress drag should be discarded while TTS settings are open",
  );
  assert.match(
    styleSource,
    /\.pageStage\.ttsSettingsOpen \.pageEdge\s*\{[^}]*pointer-events:\s*none;/s,
    "page edge navigation hit zones should be disabled while the settings panel is open",
  );
});

test("epub tts settings choose voice rate and volume for speech requests", async () => {
  const appSource = await readFile(path.join(srcDir, "App.tsx"), "utf8");

  assert.ok(
    appSource.includes("const [epubTtsSettings, setEpubTtsSettings] = useState<EpubTtsSettings>(readLocalEpubTtsSettings);"),
    "EPUB TTS settings should be app state initialized from local preferences",
  );
  assert.match(
    appSource,
    /rate:\s*epubTtsSettings\.rate,[\s\S]*voiceId:\s*epubTtsSettings\.voiceId \|\| \(voices\?\.\[0\]\?\.id \?\? ""\),[\s\S]*volume:\s*epubTtsSettings\.volume,/,
    "startEpubTts should use the selected voice, rate, and volume",
  );
  assert.match(
    appSource,
    /writeLocalEpubTtsSettings\(nextSettings\);/,
    "changing TTS settings should persist them locally",
  );
});

test("epub tts can start continuous reading from the current iframe selection", async () => {
  const appSource = await readFile(path.join(srcDir, "App.tsx"), "utf8");
  const ttsSource = await readFile(path.join(srcDir, "epub-tts.ts"), "utf8");

  assert.match(
    ttsSource,
    /export function extractEpubTtsBlocksFromSelectionStart\(\s*doc: Document,\s*spineItemId: string,\s*range: Range,/,
    "TTS extraction should expose a selection-start entry point",
  );
  assert.match(
    appSource,
    /const selectionSnapshot = options\.ignoreSelection \? null : resolveEpubTtsStartSelection\(context\);[\s\S]*extractEpubTtsBlocksFromSelectionStart\(\s*selectionSnapshot\.doc,\s*selectionSnapshot\.spineItemId,\s*selectionSnapshot\.range,\s*\)/,
    "Start TTS should prefer a valid selected-text start before falling back to the visible page",
  );
  assert.match(
    appSource,
    /onStartPointerDown=\{prepareEpubTtsSelectionStart\}/,
    "the TTS start button should capture iframe selection before the click can collapse it",
  );
  assert.match(
    appSource,
    /onTtsSelectionChange=\{handleEpubTtsSelectionChange\}/,
    "the EPUB frame should report selected text snapshots to the TTS controller",
  );
});

test("epub tts controls make a selected-text start visible without adding a new toolbar", async () => {
  const appSource = await readFile(path.join(srcDir, "App.tsx"), "utf8");

  assert.match(
    appSource,
    /selectionActive=\{Boolean\(epubTtsSelectionText\.trim\(\)\)\}/,
    "compact TTS controls should know when the next start comes from selected text",
  );
  assert.match(
    appSource,
    /selectionActive \? labels\.ttsStartSelection : labels\.ttsStart/,
    "the start button should switch labels when text selection defines the TTS start point",
  );
});

test("epub tts continues into the next spine chapter after natural queue completion", async () => {
  const appSource = await readFile(path.join(srcDir, "App.tsx"), "utf8");

  assert.ok(
    appSource.includes("const epubTtsSessionActiveRef = useRef(false);"),
    "App should keep a continuous TTS session marker while automatic chapter advance is allowed",
  );
  assert.ok(appSource.includes("const epubTtsChunksRef = useRef<EpubTtsChunk[]>([]);"));
  assert.ok(appSource.includes("const epubTtsAdvanceInFlightRef = useRef(false);"));
  assert.match(
    appSource,
    /useEffect\(\(\) => \{[\s\S]*epubTtsState\.status !== "idle"[\s\S]*epubTtsSessionActiveRef\.current[\s\S]*advanceEpubTtsToNextSpine\(\);[\s\S]*\}, \[epubTtsState\.status\]\);/,
    "natural queue idle should drive cross-chapter continuation from App state",
  );
  assert.match(
    appSource,
    /function advanceEpubTtsToNextSpine\(\)[\s\S]*epubTtsAdvanceInFlightRef\.current = true;[\s\S]*epubTtsAutoContinueRef\.current = \{[\s\S]*spineItemId: nextSpineItem\.href,[\s\S]*\};[\s\S]*setPageIndex\(nextIndex\);/,
    "natural TTS completion should schedule the next spine item and move the reader to it",
  );
  assert.match(
    appSource,
    /type EpubTtsStartOptions = \{ autoContinue\?: boolean; context\?: EpubTtsDocumentContext; ignoreSelection\?: boolean; pagePosition\?: number \};/,
    "TTS start options should allow auto-continue to pass the ready next-chapter document",
  );
  assert.match(
    appSource,
    /function startPendingEpubTtsAutoContinue\(context: EpubTtsDocumentContext\)[\s\S]*void startEpubTts\(\{ autoContinue: true, context, ignoreSelection: true, pagePosition: 0 \}\);/,
    "the expected next chapter ready signal should start TTS immediately after the new document is available",
  );
  assert.match(
    appSource,
    /const isExpectedAutoContinue = Boolean\([\s\S]*pendingAutoContinue[\s\S]*pendingAutoContinue\.spineItemId === context\.spineItemId[\s\S]*\);[\s\S]*const currentTtsSpineItemId = epubTtsReaderStateRef\.current\.epubManifest\?\.spine\[epubTtsReaderStateRef\.current\.pageIndex\]\?\.href \?\? "";/,
    "TTS document ready handling should identify the expected next chapter before applying stale callback protection",
  );
  assert.match(
    appSource,
    /if \(currentTtsSpineItemId && context\.spineItemId !== currentTtsSpineItemId && !isExpectedAutoContinue\) return;/,
    "stale EPUB iframe ready callbacks should not reject the expected next chapter's TTS",
  );
  assert.match(
    appSource,
    /const isExpectedAutoContinue = Boolean\([\s\S]*pendingAutoContinue[\s\S]*pendingAutoContinue\.spineItemId === context\.spineItemId[\s\S]*\);[\s\S]*epubTtsSpineRef\.current !== context\.spineItemId && !isExpectedAutoContinue/,
    "the expected next-chapter iframe should not be treated as a manual chapter switch that stops TTS",
  );
  const advanceBody = appSource.match(/function advanceEpubTtsToNextSpine\(\)[\s\S]*?function handleEpubTtsDocumentReady/)?.[0] ?? "";
  assert.match(
    advanceBody,
    /setPageIndex\(nextIndex\);\s*scheduleEpubTtsAutoContinueStart\(\);/,
    "automatic chapter restart should keep retrying after the page turn in case the next iframe ready signal is missed",
  );
  assert.match(
    appSource,
    /function scheduleEpubTtsAutoContinueStart\(\)[\s\S]*epubTtsAutoContinueTimerRef[\s\S]*startPendingEpubTtsAutoContinue\(context\)/,
    "automatic chapter restart should retry until the new iframe context is available",
  );
  assert.match(
    appSource,
    /const EPUB_TTS_AUTO_CONTINUE_RETRY_LIMIT = 300;/,
    "automatic chapter restart should keep pending state long enough for slower EPUB iframe reloads",
  );
  assert.match(
    appSource,
    /const blocks = usingSelectionStart \? selectionBlocks : extractEpubTtsBlocks\(context\.doc, context\.spineItemId, \{[\s\S]*pagePosition: options\.pagePosition \?\? epubPagePosition,/,
    "automatic next-chapter TTS should not reuse the previous chapter's final page position",
  );
  assert.match(
    appSource,
    /const ttsBook = options\.autoContinue \? epubTtsReaderStateRef\.current\.selectedBook : selectedBook;[\s\S]*if \(!ttsBook \|\| ttsBook\.format !== "epub"\) return;/,
    "automatic next-chapter TTS should read the latest selected book from a ref instead of a stale callback closure",
  );
  assert.match(
    appSource,
    /if \(!chunks\.length\) \{[\s\S]*if \(options\.autoContinue\) \{[\s\S]*setEpubTtsState\(idleEpubTtsState\(\)\);[\s\S]*scheduleEpubTtsAutoContinueStart\(\);[\s\S]*return;/,
    "an early empty extraction during automatic continuation should retry instead of clearing the pending chapter",
  );
  assert.match(
    appSource,
    /if \(options\.autoContinue\) \{[\s\S]*cancelEpubTtsAutoContinueRetry\(\);[\s\S]*epubTtsAutoContinueRef\.current = null;[\s\S]*\}[\s\S]*const queue = ensureEpubTtsQueue\(\);/,
    "automatic continuation should clear pending state only after readable chunks are available",
  );
  assert.match(
    appSource,
    /skipCancel:\s*Boolean\(options\.autoContinue\),/,
    "automatic next-chapter TTS should preserve the active browser speech session instead of resetting it",
  );
  assert.match(
    appSource,
    /function stopEpubTts\(\) \{[\s\S]*clearEpubTtsContinuousSession\(\);/,
    "manual stop should cancel any pending or active cross-chapter continuation",
  );
  assert.match(
    appSource,
    /epubTtsSessionActiveRef\.current = true;[\s\S]*epubTtsChunksRef\.current = chunks;[\s\S]*epubTtsAdvanceInFlightRef\.current = false;/,
    "starting TTS should mark the continuous session and remember the chunks that can naturally complete",
  );
});
