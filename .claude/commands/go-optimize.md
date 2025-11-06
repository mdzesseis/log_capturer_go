# Go Performance Optimization

Use the Task tool to launch the golang agent with the following prompt:

You are tasked with optimizing Go code for performance in the log_capturer_go project. Focus on:

1. **Memory Optimization**:
   - Identify unnecessary allocations
   - Implement object pools where appropriate
   - Optimize string operations
   - Reduce GC pressure
   - Check for memory leaks

2. **Concurrency Improvements**:
   - Review goroutine management
   - Optimize channel usage
   - Check for race conditions
   - Implement proper worker pools
   - Ensure context propagation

3. **Performance Profiling**:
   - Run CPU and memory profiling
   - Identify hot paths
   - Optimize critical sections
   - Reduce lock contention
   - Implement batching where beneficial

4. **Code Review**:
   - Review the code in: {{FILE_PATH}}
   - Identify performance bottlenecks
   - Suggest specific optimizations
   - Provide benchmarks for improvements

Please analyze the code and provide:
- Specific performance issues found
- Recommended optimizations with code examples
- Expected performance improvements
- Benchmark comparisons where possible

Use the following command to invoke the agent:
```
subagent_type: "golang"
description: "Optimize Go code for performance"
```

Focus on practical, measurable improvements that maintain code readability and correctness.