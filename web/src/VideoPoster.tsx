import { useState } from "react";
import { videoPosterSource } from "./video-poster-cache";

// All video browsing surfaces share the metadata artwork endpoint. Unlike the
// legacy thumbnail endpoint, it never starts ffmpeg to generate a frame grab.
export function VideoPoster({ videoId, revision = 0 }: { videoId: number; revision?: number }) {
  // The endpoint revalidates with ETag / private,no-cache. A mount timestamp
  // defeated that cache and downloaded the same poster on every navigation.
  const src = videoPosterSource(videoId, revision);
  return <PosterImage key={src} src={src} />;
}

function PosterImage({ src }: { src: string }) {
  const [failed, setFailed] = useState(false);
  if (failed) return <svg className="videoPosterFallback" viewBox="0 0 300 450" aria-hidden="true">
    <rect width="300" height="450" fill="#26334b" />
    <path d="M125 190v70l65-35z" fill="#91a9d0" />
  </svg>;
  return <img src={src} alt="" loading="lazy" decoding="async" onError={() => setFailed(true)} />;
}
