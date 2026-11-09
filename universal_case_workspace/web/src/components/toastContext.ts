import { createContext, useContext } from "react";

export const ToastContext = createContext<(msg: string) => void>(() => undefined);

export function useToast() {
  return useContext(ToastContext);
}
