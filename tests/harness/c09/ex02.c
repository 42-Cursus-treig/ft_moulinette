#include <stdlib.h>
#include <unistd.h>

char	**ft_split(char *str, char *charset);

static void	put_str(char *s)
{
	while (s && *s)
		write(1, s++, 1);
}

int	main(int argc, char **argv)
{
	char	**res;
	int		i;

	if (argc != 3)
		return (0);
	res = ft_split(argv[1], argv[2]);
	if (!res)
		return (1);
	i = 0;
	while (res[i])
	{
		put_str(res[i]);
		if (res[i + 1])
			write(1, "\n", 1);
		i++;
	}
	return (0);
}
