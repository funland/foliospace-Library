import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';
const source=readFileSync(new URL('./video-details.ts',import.meta.url),'utf8');
const js=ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.ES2020}}).outputText;
const {videoSpecs,videoProgressLabel}=await import(`data:text/javascript;base64,${Buffer.from(js).toString('base64')}`);
test('file specifications hide unknown values without inventing HDR or subtitles',()=>{
 assert.deepEqual(videoSpecs({format:'mkv',width:3840,height:2160,videoCodec:'hevc',audioCodec:'unknown',durationSeconds:6000,size:1024**3},true),['MKV','3840 × 2160','HEVC','100 分钟','1.00 GiB']);
 assert.deepEqual(videoSpecs({},true),[]);
});
test('saved video progress distinguishes completion, no progress and changed edition',()=>{
 assert.equal(videoProgressLabel({completed:false,positionSeconds:0,durationSeconds:0},true),'');
 assert.match(videoProgressLabel({completed:false,positionSeconds:300,durationSeconds:1000},true),/30%/);
 assert.equal(videoProgressLabel({completed:true,positionSeconds:900,durationSeconds:1000},true),'已看完');
 assert.match(videoProgressLabel({completed:true,positionSeconds:900,durationSeconds:1000,identityChanged:true},true),/版本已变化/);
});
