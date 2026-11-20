/** Shared keyboard behavior for the existing service-owned mobile navigation drawers. */
export function manageDrawerFocus(drawer:HTMLElement,trigger:HTMLElement,onClose:()=>void){
  const focusable=()=>Array.from(drawer.querySelectorAll<HTMLElement>('a[href],button:not(:disabled),input:not(:disabled),select:not(:disabled),[tabindex="0"]')).filter(element=>element.getClientRects().length>0);
  focusable()[0]?.focus();
  const onKey=(event:KeyboardEvent)=>{
    if(event.key==="Escape"){event.preventDefault();onClose();return;}
    if(event.key!=="Tab")return;
    const items=focusable(),first=items[0],last=items[items.length-1];
    if(!first||!last){event.preventDefault();return;}
    if(event.shiftKey&&(document.activeElement===first||!drawer.contains(document.activeElement))){event.preventDefault();last.focus();}
    else if(!event.shiftKey&&(document.activeElement===last||!drawer.contains(document.activeElement))){event.preventDefault();first.focus();}
  };
  document.addEventListener("keydown",onKey);
  return()=>{document.removeEventListener("keydown",onKey);if(trigger.isConnected)trigger.focus();};
}
