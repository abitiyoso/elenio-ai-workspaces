// Ship a verified source snapshot so the existing isolated Docker build stays usable.
// Canonical design tokens and approved artwork remain in the monorepo.
import {createHash} from 'node:crypto';
import {existsSync,readFileSync,writeFileSync,mkdirSync} from 'node:fs';
import {dirname,resolve} from 'node:path';
import {fileURLToPath} from 'node:url';
const app=resolve(dirname(fileURLToPath(import.meta.url)),'..');
const repo=resolve(app,'../../..');
const assets=[
  ['packages/ui/tokens.css','src/ui/elenio-tokens.css'],
  ['flowcase_platform/web/public/brand/elenio-logo.png','public/brand/elenio-logo.png'],
  ['flowcase_platform/web/node_modules/@fontsource-variable/inter/files/inter-latin-wght-normal.woff2','src/ui/assets/inter-latin-wght-normal.woff2'],
  ['flowcase_platform/web/node_modules/@fontsource-variable/inter/LICENSE','public/brand/INTER-LICENSE.txt'],
];
// Git may check text out with CRLF on Windows. Pin normalized text, exact binary assets.
const digest=(bytes,file)=>createHash('sha256').update(/\.(css|txt)$/.test(file)?bytes.toString('utf8').replace(/\r\n/g,'\n'):bytes).digest('hex');
const manifestPath=resolve(app,'shared-ui.json');
if(process.argv.includes('--sync')){
  const manifest={schema_version:1,text_normalization:'LF',assets:[]};
  for(const [canonical,file] of assets){
    const bytes=readFileSync(resolve(repo,canonical));
    mkdirSync(dirname(resolve(app,file)),{recursive:true});
    writeFileSync(resolve(app,file),bytes);
    manifest.assets.push({file,canonical,sha256:digest(bytes,file)});
  }
  writeFileSync(manifestPath,JSON.stringify(manifest,null,2)+'\n');
}
const manifest=JSON.parse(readFileSync(manifestPath,'utf8'));
if(manifest.schema_version!==1||manifest.text_normalization!=='LF'||manifest.assets.length!==assets.length)throw new Error('Invalid shared UI manifest');
for(const [canonical,file] of assets){
  const pinned=manifest.assets.find(item=>item.file===file&&item.canonical===canonical);
  if(!pinned||digest(readFileSync(resolve(app,file)),file)!==pinned.sha256)throw new Error(`Shared UI snapshot changed: ${file}`);
  const source=resolve(repo,canonical);
  if(existsSync(source)&&digest(readFileSync(source),file)!==pinned.sha256)throw new Error(`Shared UI drift: ${canonical}; synchronize explicitly`);
}
console.log('Shared UI tokens, unchanged logo, bundled font and license verified.');
