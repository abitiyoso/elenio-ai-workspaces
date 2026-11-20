// Canonical snapshot verifier for independently packaged original KMS and UCW frontends.
// Run each app's scripts/shared-ui.mjs; --sync is an explicit reviewed asset update.
import {createHash} from 'node:crypto';
import {existsSync,readFileSync,writeFileSync,mkdirSync} from 'node:fs';
import {dirname,resolve} from 'node:path';
import {fileURLToPath} from 'node:url';
const app=resolve(dirname(fileURLToPath(import.meta.url)),'..');
const candidate=resolve(app,'../..');
const repo=existsSync(resolve(candidate,'packages/ui/tokens.css'))?candidate:null;
const assets=[
  ['packages/ui/tokens.css','src/design/tokens.css'],
  ['packages/ui/mobileNavigation.ts','src/design/mobileNavigation.ts'],
  ['packages/ui/scripts/prepare-workbench.mjs','scripts/shared-ui.mjs'],
  ['flowcase_platform/web/public/brand/elenio-logo.png','public/brand/elenio-logo.png'],
  ['flowcase_platform/web/public/brand/elenio-icon.png','public/brand/elenio-icon.png'],
  ['flowcase_platform/web/node_modules/@fontsource-variable/inter/files/inter-latin-wght-normal.woff2','src/design/inter-latin-wght-normal.woff2'],
  ['flowcase_platform/web/node_modules/@fontsource-variable/inter/LICENSE','public/brand/INTER-LICENSE.txt'],
];
const digest=(bytes,file)=>createHash('sha256').update(/\.(css|txt|mjs|ts)$/.test(file)?bytes.toString('utf8').replace(/\r\n/g,'\n'):bytes).digest('hex');
const manifestPath=resolve(app,'shared-ui.json');
if(process.argv.includes('--sync')){
  if(!repo)throw new Error('Synchronize shared assets from the original monorepo.');
  const manifest={schema_version:1,text_normalization:'LF',assets:[]};
  for(const [canonical,file] of assets){
    const bytes=readFileSync(resolve(repo,canonical));
    mkdirSync(dirname(resolve(app,file)),{recursive:true});writeFileSync(resolve(app,file),bytes);
    manifest.assets.push({file,canonical,sha256:digest(bytes,file)});
  }
  writeFileSync(manifestPath,JSON.stringify(manifest,null,2)+'\n');
}
const manifest=JSON.parse(readFileSync(manifestPath,'utf8'));
if(manifest.schema_version!==1||manifest.text_normalization!=='LF'||manifest.assets.length!==assets.length)throw new Error('Invalid shared UI manifest');
for(const [canonical,file] of assets){
  const pinned=manifest.assets.find(item=>item.file===file&&item.canonical===canonical);
  if(!pinned||digest(readFileSync(resolve(app,file)),file)!==pinned.sha256)throw new Error(`Shared UI snapshot changed: ${file}`);
  if(repo&&existsSync(resolve(repo,canonical))&&digest(readFileSync(resolve(repo,canonical)),file)!==pinned.sha256)throw new Error(`Shared UI drift: ${canonical}; synchronize explicitly`);
}
console.log('Shared tokens, unchanged official logo, bundled Inter and license verified.');
