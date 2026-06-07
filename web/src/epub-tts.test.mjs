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
    /<button onClick=\{returnToLibrary\}>\{t\.backToShelf\}<\/button>\s*\{selectedBook\.format === "epub" && \(\s*<EpubTtsControls/s,
    "EPUB TTS controls should be in the toolbar immediately after the back-to-shelf button",
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
    /\{readerFullscreen && selectedBook\.format === "epub" && \(\s*<div className="readerFullscreenTts"/s,
    "EPUB fullscreen should expose a dedicated TTS control island while the main header is hidden",
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
    /\{epubTtsSettingsOpen && \(\s*<EpubTtsSettingsPanel/s,
    "the TTS settings panel should render separately from the compact toolbar controls",
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
