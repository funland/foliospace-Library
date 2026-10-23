export function videoSpecs(video: { format?: string; size?: number; durationSeconds?: number; width?: number; height?: number; videoCodec?: string; audioCodec?: string }, zh: boolean): string[] {
 const values: string[] = [];
 if (video.format) values.push(video.format.toUpperCase());
 if (video.width && video.height) values.push(`${video.width} × ${video.height}`);
 for (const codec of [video.videoCodec, video.audioCodec]) if (codec && !/^(unknown|none|n\/a)$/i.test(codec)) values.push(codec.toUpperCase());
 if (video.durationSeconds && video.durationSeconds > 0) values.push(`${Math.round(video.durationSeconds / 60)} ${zh ? "分钟" : "min"}`);
 if (video.size && video.size > 0) values.push(video.size >= 1024 ** 3 ? `${(video.size / 1024 ** 3).toFixed(2)} GiB` : `${(video.size / 1024 ** 2).toFixed(1)} MiB`);
 return values;
}
export function videoProgressLabel(state: { completed: boolean; positionSeconds: number; durationSeconds: number; identityChanged?: boolean }, zh: boolean): string {
 if (state.identityChanged) return zh ? "文件版本已变化，原进度保留" : "Version changed; previous progress retained";
 if (state.completed) return zh ? "已看完" : "Watched";
 if (state.positionSeconds <= 0) return "";
 const minutes = Math.floor(state.positionSeconds / 60);
 return state.durationSeconds > 0 ? `${zh ? "已观看" : "Watched"} ${Math.min(100, Math.round(state.positionSeconds / state.durationSeconds * 100))}% · ${minutes} ${zh ? "分钟" : "min"}` : `${zh ? "已观看" : "Watched"} ${minutes} ${zh ? "分钟" : "min"}`;
}
