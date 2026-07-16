import { create } from "zustand";
import { persist } from "zustand/middleware";

export interface ChatMessage {
  id: string;
  role: "user" | "assistant";
  content: string;
  timestamp: number;
}

interface ChatState {
  messages: ChatMessage[];
  addMessage: (msg: Omit<ChatMessage, "id" | "timestamp"> & { content: string }) => string;
  updateMessage: (id: string, content: string) => void;
  clearMessages: () => void;
}

export const useChatStore = create<ChatState>()(
  persist(
    (set) => ({
      messages: [],

      addMessage: (msg) => {
        const id = crypto.randomUUID();
        const entry: ChatMessage = {
          ...msg,
          id,
          timestamp: Date.now(),
        };
        set((s) => ({ messages: [...s.messages, entry] }));
        return id;
      },

      updateMessage: (id, content) =>
        set((s) => ({
          messages: s.messages.map((m) =>
            m.id === id ? { ...m, content } : m
          ),
        })),

      clearMessages: () => set({ messages: [] }),
    }),
    {
      name: "dynamo-sage-chat",
    }
  )
);
