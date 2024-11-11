# Audio-LLM-Contextual-Heygen

A query engine to provide streams of LLM generated Text to Audio with extreme contextual awareness of current conversation using smaller LLM's with lower context window and costs

System Architecture : 
![Screenshot 2024-11-11 at 1 52 20 AM](https://github.com/user-attachments/assets/6c37449a-13e3-4583-8776-a4abfa0e6e3d)

Heygen Interactive Avatar for Zoom calls : 
- Integrating this service/concept could lead to increased performance, and a fair cost reduction in certain scenarios.

Prediction Service : 
- At every query received, it caches the most related embedding chunks in a redis DB using an adaptive caching mechanism
- For subsequent queries, it searches the cache for relevant information and responds immediately.
- Cache is based on Least-Recently-Used mechanism, holds 5 chunks at a time for a given request.

Why? 
- Can reduce costs drastically by limiting calls to the main LLM model.
- Queries can be redirected based on similarity scores and relevance to smaller LLM models with the same accuracy.

Stretch Goal : Integrate advanced audio codecs like AV1 and reduce bitrate for transmission during network congestions, expected in Zoom calls and provides better user experience.
